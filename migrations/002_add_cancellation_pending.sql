-- +goose Up
-- Поддержка Compensating Transaction Pattern для отмены бронирования.
-- previous_status хранит статус до начала отмены и используется для rollback,
-- если Catalog не смог обработать команду CancelBookingJobByRequestIdRequest.
-- cancellation_requested_at фиксирует момент отправки команды отмены в Catalog
-- (для мониторинга и обнаружения зависших cancellation_pending).
ALTER TABLE bookings
    ADD COLUMN previous_status          VARCHAR(30),
    ADD COLUMN cancellation_requested_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE bookings
    DROP COLUMN IF EXISTS cancellation_requested_at,
    DROP COLUMN IF EXISTS previous_status;
