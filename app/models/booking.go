package models

import "time"

// BookingStatus представляет статус бронирования.
type BookingStatus string

const (
	BookingStatusAwaitsConfirmation  BookingStatus = "awaits_confirmation"
	BookingStatusConfirmed           BookingStatus = "confirmed"
	BookingStatusCancellationPending BookingStatus = "cancellation_pending"
	BookingStatusCancelled           BookingStatus = "cancelled"
)

// IsValid проверяет, что статус принадлежит допустимому множеству.
func (s BookingStatus) IsValid() bool {
	switch s {
	case BookingStatusAwaitsConfirmation,
		BookingStatusConfirmed,
		BookingStatusCancellationPending,
		BookingStatusCancelled:
		return true
	default:
		return false
	}
}

// Booking -- доменная сущность бронирования.
// Поля неэкспортируемые для обеспечения инкапсуляции.
type Booking struct {
	id                      int64
	status                  BookingStatus
	userID                  int64
	resourceID              int64
	startDate               time.Time
	endDate                 time.Time
	createdAt               time.Time
	previousStatus          *BookingStatus
	cancellationRequestedAt *time.Time
}

func (b *Booking) ID() int64             { return b.id }
func (b *Booking) Status() BookingStatus { return b.status }
func (b *Booking) UserID() int64         { return b.userID }
func (b *Booking) ResourceID() int64     { return b.resourceID }
func (b *Booking) StartDate() time.Time  { return b.startDate }
func (b *Booking) EndDate() time.Time    { return b.endDate }
func (b *Booking) CreatedAt() time.Time  { return b.createdAt }

// PreviousStatus возвращает статус, в котором находилось бронирование
// до начала отмены. nil, если бронирование не в процессе отмены.
func (b *Booking) PreviousStatus() *BookingStatus { return b.previousStatus }

// CancellationRequestedAt возвращает время отправки команды отмены в Catalog.
func (b *Booking) CancellationRequestedAt() *time.Time { return b.cancellationRequestedAt }

// NewBooking создаёт новое бронирование в статусе AwaitsConfirmation.
func NewBooking(userID, resourceID int64, startDate, endDate time.Time) (*Booking, error) {
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if resourceID <= 0 {
		return nil, ErrInvalidResourceID
	}
	if startDate.IsZero() || endDate.IsZero() {
		return nil, ErrInvalidDateRange
	}
	if !endDate.After(startDate) {
		return nil, ErrEndDateBeforeStartDate
	}

	return &Booking{
		status:     BookingStatusAwaitsConfirmation,
		userID:     userID,
		resourceID: resourceID,
		startDate:  startDate,
		endDate:    endDate,
		createdAt:  time.Now(),
	}, nil
}

// Confirm подтверждает бронирование.
// Допустимый переход: AwaitsConfirmation -> Confirmed.
func (b *Booking) Confirm() error {
	if b.status != BookingStatusAwaitsConfirmation {
		return ErrInvalidStatusTransition
	}
	b.status = BookingStatusConfirmed
	return nil
}

// StartCancellation переводит бронирование в промежуточный статус CancellationPending
// и фиксирует предыдущий статус для возможного rollback.
//
// Допустимые переходы:
//   - AwaitsConfirmation -> CancellationPending
//   - Confirmed          -> CancellationPending (только если StartDate > today)
//
// requestedAt сохраняется как время отправки команды отмены в Catalog;
// используется для последующего наблюдения за тайм-аутами / DLQ.
func (b *Booking) StartCancellation(today, requestedAt time.Time) error {
	switch b.status {
	case BookingStatusAwaitsConfirmation:
		// разрешено
	case BookingStatusConfirmed:
		if !b.startDate.After(today) {
			return ErrCannotCancelPastBooking
		}
	default:
		return ErrInvalidStatusTransition
	}

	prev := b.status
	b.previousStatus = &prev
	b.cancellationRequestedAt = &requestedAt
	b.status = BookingStatusCancellationPending
	return nil
}

// CompleteCancel переводит бронирование из CancellationPending в Cancelled.
// Вызывается, когда Catalog подтвердил отмену соответствующего booking job.
func (b *Booking) CompleteCancel() error {
	if b.status != BookingStatusCancellationPending {
		return ErrInvalidStatusTransition
	}
	b.status = BookingStatusCancelled
	b.previousStatus = nil
	b.cancellationRequestedAt = nil
	return nil
}

// MarkRejected мгновенно переводит ожидающее подтверждения бронирование в Cancelled.
// Используется, когда Catalog отверг создание booking job (BookingJobDenied):
// в Catalog'е нет соответствующего job, поэтому компенсация не требуется.
//
// Допустимый переход: AwaitsConfirmation -> Cancelled.
func (b *Booking) MarkRejected() error {
	if b.status != BookingStatusAwaitsConfirmation {
		return ErrInvalidStatusTransition
	}
	b.status = BookingStatusCancelled
	return nil
}

// RollbackCancellation возвращает бронирование в статус, который был до StartCancellation.
// Вызывается, когда Catalog не смог обработать команду отмены (DLQ / ошибка).
func (b *Booking) RollbackCancellation() error {
	if b.status != BookingStatusCancellationPending || b.previousStatus == nil {
		return ErrInvalidStatusTransition
	}
	b.status = *b.previousStatus
	b.previousStatus = nil
	b.cancellationRequestedAt = nil
	return nil
}

// RestoreBooking восстанавливает Booking из данных хранилища.
// Используется только в слое storage для маппинга строк БД на доменный объект.
func RestoreBooking(
	id int64,
	status BookingStatus,
	userID, resourceID int64,
	startDate, endDate, createdAt time.Time,
	previousStatus *BookingStatus,
	cancellationRequestedAt *time.Time,
) *Booking {
	return &Booking{
		id:                      id,
		status:                  status,
		userID:                  userID,
		resourceID:              resourceID,
		startDate:               startDate,
		endDate:                 endDate,
		createdAt:               createdAt,
		previousStatus:          previousStatus,
		cancellationRequestedAt: cancellationRequestedAt,
	}
}
