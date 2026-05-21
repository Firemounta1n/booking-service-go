package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"booking-service/app/service"
)

// CancelBookingErrorHandler обрабатывает сообщения о провале команды отмены booking job:
// событие CancelBookingJobFailed либо сообщения, попавшие в DLQ.
//
// В обоих случаях handler инициирует rollback бронирования: возвращает booking
// из промежуточного статуса cancellation_pending в исходный статус.
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

// Handle десериализует входящее сообщение, извлекает RequestId и делегирует
// откат сервису. Поддерживает как явное событие CancelBookingJobFailed, так
// и исходное CancelBookingJobCommand из DLQ -- у обоих структура содержит RequestId.
func (h *CancelBookingErrorHandler) Handle(ctx context.Context, body []byte) error {
	requestID, reason, err := parseCancelErrorPayload(body)
	if err != nil {
		return fmt.Errorf("разбор сообщения об ошибке отмены: %w", err)
	}

	h.logger.Warn("получен сигнал об ошибке отмены booking job",
		zap.String("requestId", requestID),
		zap.String("reason", reason),
	)

	if err := h.service.HandleCancelError(ctx, requestID); err != nil {
		return fmt.Errorf("rollback отмены бронирования (requestId=%s): %w", requestID, err)
	}

	return nil
}

// parseCancelErrorPayload пытается извлечь RequestId и причину из тела сообщения.
// Поддерживает оба формата: событие CancelBookingJobFailed (с полем Reason)
// и исходную команду CancelBookingJobCommand из DLQ (без Reason).
func parseCancelErrorPayload(body []byte) (requestID, reason string, err error) {
	var payload struct {
		RequestId string `json:"RequestId"`
		Reason    string `json:"Reason"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", err
	}
	if payload.RequestId == "" {
		return "", "", fmt.Errorf("пустой RequestId в сообщении об ошибке отмены")
	}
	return payload.RequestId, payload.Reason, nil
}
