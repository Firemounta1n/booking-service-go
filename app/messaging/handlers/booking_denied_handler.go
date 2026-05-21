package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"booking-service/app/messaging"
	"booking-service/app/service"
)

// BookingDeniedHandler обрабатывает события BookingJobDenied.
type BookingDeniedHandler struct {
	service *service.BookingsService
	logger  *zap.Logger
}

// NewBookingDeniedHandler создаёт новый обработчик.
func NewBookingDeniedHandler(svc *service.BookingsService, logger *zap.Logger) *BookingDeniedHandler {
	return &BookingDeniedHandler{
		service: svc,
		logger:  logger,
	}
}

// Handle обрабатывает событие отклонения бронирования.
func (h *BookingDeniedHandler) Handle(ctx context.Context, body []byte) error {
	var event messaging.BookingJobDenied
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("десериализация BookingJobDenied: %w", err)
	}

	bookingID, err := messaging.RequestIDToBookingID(event.RequestId)
	if err != nil {
		return fmt.Errorf("извлечение bookingId из RequestId: %w", err)
	}

	h.logger.Info("получено событие BookingJobDenied",
		zap.Int64("bookingId", bookingID),
		zap.Int64("catalogJobId", event.Id),
		zap.String("reason", event.Reason),
	)

	if err := h.service.MarkRejected(ctx, bookingID); err != nil {
		return fmt.Errorf("отметка отклонения бронирования %d: %w", bookingID, err)
	}

	h.logger.Info("бронирование отклонено через событие",
		zap.Int64("bookingId", bookingID),
		zap.String("reason", event.Reason),
	)
	return nil
}
