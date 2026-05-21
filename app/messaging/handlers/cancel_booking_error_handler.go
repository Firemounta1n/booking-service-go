package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"booking-service/app/messaging"
	"booking-service/app/service"
)

// CancelBookingErrorHandler обрабатывает сообщения CancelBookingJobByRequestIdRequest,
// перенаправленные RabbitMQ в dead-letter-queue после исчерпания попыток на стороне Catalog.
//
// Handler инициирует компенсацию: возвращает booking из промежуточного статуса
// cancellation_pending в исходный статус, чтобы не было рассинхронизации с Catalog.
type CancelBookingErrorHandler struct {
	service *service.BookingsService
	logger  *zap.Logger
}

// NewCancelBookingErrorHandler создаёт новый обработчик ошибок отмены.
func NewCancelBookingErrorHandler(svc *service.BookingsService, logger *zap.Logger) *CancelBookingErrorHandler {
	return &CancelBookingErrorHandler{
		service: svc,
		logger:  logger,
	}
}

// Handle десериализует исходную команду из DLQ и делегирует rollback сервису.
func (h *CancelBookingErrorHandler) Handle(ctx context.Context, body []byte) error {
	var cmd messaging.CancelBookingJobCommand
	if err := json.Unmarshal(body, &cmd); err != nil {
		return fmt.Errorf("десериализация CancelBookingJobCommand из DLQ: %w", err)
	}
	if cmd.RequestId == "" {
		return fmt.Errorf("пустой RequestId в сообщении DLQ")
	}

	h.logger.Warn("получено сообщение CancelBookingJob из DLQ -- запускаем rollback",
		zap.String("requestId", cmd.RequestId),
	)

	if err := h.service.HandleCancelError(ctx, cmd.RequestId); err != nil {
		return fmt.Errorf("rollback отмены бронирования (requestId=%s): %w", cmd.RequestId, err)
	}

	return nil
}
