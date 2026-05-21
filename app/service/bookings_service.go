package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"booking-service/app/api/dto"
	"booking-service/app/messaging"
	"booking-service/app/models"
)

// BookingsService обрабатывает команды (изменение состояния) для бронирований.
//
// Этот сервис -- оркестратор: он координирует домен и репозиторий,
// но НЕ содержит бизнес-правила (они в models.Booking).
type BookingsService struct {
	repo      models.BookingRepository
	publisher *messaging.Publisher
	logger    *zap.Logger
}

// NewBookingsService создаёт новый BookingsService.
func NewBookingsService(repo models.BookingRepository, publisher *messaging.Publisher, logger *zap.Logger) *BookingsService {
	return &BookingsService{
		repo:      repo,
		publisher: publisher,
		logger:    logger,
	}
}

// Create создаёт новое бронирование.
//
// Шаги:
//  1. Парсинг дат из строкового формата
//  2. Создание доменного объекта (валидация в конструкторе)
//  3. Сохранение в БД
//  4. Публикация команды в Catalog
//  5. Возврат ID
func (s *BookingsService) Create(ctx context.Context, req dto.CreateBookingRequest) (int64, error) {
	startDate, err := time.Parse(dto.DateFormat, req.StartDate)
	if err != nil {
		return 0, fmt.Errorf("некорректный формат startDate: %w", err)
	}

	endDate, err := time.Parse(dto.DateFormat, req.EndDate)
	if err != nil {
		return 0, fmt.Errorf("некорректный формат endDate: %w", err)
	}

	booking, err := models.NewBooking(req.UserID, req.ResourceID, startDate, endDate)
	if err != nil {
		return 0, err
	}

	id, err := s.repo.Create(ctx, booking)
	if err != nil {
		return 0, fmt.Errorf("сохранение бронирования: %w", err)
	}

	s.logger.Info("бронирование создано",
		zap.Int64("id", id),
		zap.Int64("userId", req.UserID),
		zap.Int64("resourceId", req.ResourceID),
	)

	if err := s.publisher.PublishCreateBookingJob(ctx, messaging.CreateBookingJobCommand{
		EventId:    messaging.NewMessageID(),
		RequestId:  messaging.BookingIDToRequestID(id),
		ResourceId: req.ResourceID,
		StartDate:  req.StartDate,
		EndDate:    req.EndDate,
	}); err != nil {
		s.logger.Error("ошибка публикации CreateBookingJob", zap.Error(err), zap.Int64("bookingId", id))
		// Не возвращаем ошибку -- бронирование уже создано, команда может быть обработана позже
	}

	return id, nil
}

// Cancel переводит бронирование в промежуточный статус cancellation_pending
// (Compensating Transaction Pattern) и публикует команду отмены в Catalog.
//
// Если Catalog успешно обработает команду — отдельный success-обработчик завершит
// отмену (см. CompleteCancel). Если команда попадёт в DLQ или будет отвергнута —
// CancelBookingErrorHandler вызовет HandleCancelError и вернёт статус обратно.
//
// Шаги:
//  1. Загрузка бронирования из БД
//  2. Доменный переход StartCancellation (с фиксацией previousStatus и времени)
//  3. Сохранение обновлённого состояния
//  4. Публикация команды в Catalog
func (s *BookingsService) Cancel(ctx context.Context, id int64) error {
	booking, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	now := time.Now()
	if err := booking.StartCancellation(now, now); err != nil {
		return err
	}

	if err := s.repo.Update(ctx, booking); err != nil {
		return fmt.Errorf("обновление бронирования: %w", err)
	}

	s.logger.Info("инициирована отмена бронирования",
		zap.Int64("id", id),
		zap.String("status", string(booking.Status())),
	)

	if err := s.publisher.PublishCancelBookingJob(ctx, messaging.CancelBookingJobCommand{
		EventId:   messaging.NewMessageID(),
		RequestId: messaging.BookingIDToRequestID(id),
	}); err != nil {
		s.logger.Error("ошибка публикации CancelBookingJob", zap.Error(err), zap.Int64("bookingId", id))
	}

	return nil
}

// Confirm подтверждает бронирование по ID.
// Используется обработчиком событий RabbitMQ.
func (s *BookingsService) Confirm(ctx context.Context, id int64) error {
	booking, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := booking.Confirm(); err != nil {
		return err
	}

	if err := s.repo.Update(ctx, booking); err != nil {
		return fmt.Errorf("обновление бронирования: %w", err)
	}

	s.logger.Info("бронирование подтверждено", zap.Int64("id", id))

	return nil
}

// MarkRejected отмечает бронирование как отменённое в ответ на BookingJobDenied
// (отказ Catalog'а создать job). Никакой компенсирующей команды не публикуется --
// job в Catalog никогда не существовал.
func (s *BookingsService) MarkRejected(ctx context.Context, id int64) error {
	booking, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := booking.MarkRejected(); err != nil {
		return err
	}

	if err := s.repo.Update(ctx, booking); err != nil {
		return fmt.Errorf("обновление бронирования: %w", err)
	}

	s.logger.Info("бронирование отклонено Catalog'ом", zap.Int64("id", id))
	return nil
}

// CompleteCancel завершает Compensating Transaction Pattern для отмены:
// переводит бронирование из cancellation_pending в cancelled.
// Вызывается после получения подтверждения от Catalog об успешной отмене booking job.
func (s *BookingsService) CompleteCancel(ctx context.Context, id int64) error {
	booking, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := booking.CompleteCancel(); err != nil {
		return err
	}

	if err := s.repo.Update(ctx, booking); err != nil {
		return fmt.Errorf("обновление бронирования: %w", err)
	}

	s.logger.Info("отмена бронирования завершена", zap.Int64("id", id))
	return nil
}

// HandleCancelError выполняет компенсацию: если Catalog не смог обработать
// CancelBookingJobByRequestIdRequest (DLQ / ошибка), возвращаем бронирование
// в предыдущий статус, чтобы не было рассинхронизации с Catalog.
//
// requestID -- детерминированный UUID, в который закодирован bookingID
// (см. messaging.BookingIDToRequestID).
func (s *BookingsService) HandleCancelError(ctx context.Context, requestID string) error {
	bookingID, err := messaging.RequestIDToBookingID(requestID)
	if err != nil {
		return fmt.Errorf("извлечение bookingId из RequestId: %w", err)
	}

	booking, err := s.repo.GetByID(ctx, bookingID)
	if err != nil {
		return err
	}

	if err := booking.RollbackCancellation(); err != nil {
		// Booking уже не в cancellation_pending: либо отмена уже завершена,
		// либо rollback был выполнен ранее. Идемпотентно завершаем без ошибки.
		s.logger.Warn("rollback пропущен: бронирование не в cancellation_pending",
			zap.Int64("bookingId", bookingID),
			zap.String("status", string(booking.Status())),
		)
		return nil
	}

	if err := s.repo.Update(ctx, booking); err != nil {
		return fmt.Errorf("сохранение отката бронирования: %w", err)
	}

	s.logger.Info("выполнен откат отмены бронирования",
		zap.Int64("id", bookingID),
		zap.String("status", string(booking.Status())),
	)
	return nil
}
