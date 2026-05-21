package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"booking-service/app/models"
)

func TestNewBooking_Success(t *testing.T) {
	// Arrange
	userID := int64(1)
	resourceID := int64(10)
	startDate := time.Now().AddDate(0, 0, 7)
	endDate := time.Now().AddDate(0, 0, 14)

	// Act
	booking, err := models.NewBooking(userID, resourceID, startDate, endDate)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, models.BookingStatusAwaitsConfirmation, booking.Status())
	assert.Equal(t, userID, booking.UserID())
	assert.Equal(t, resourceID, booking.ResourceID())
	assert.Nil(t, booking.PreviousStatus())
	assert.Nil(t, booking.CancellationRequestedAt())
}

func TestNewBooking_InvalidUserID(t *testing.T) {
	_, err := models.NewBooking(0, 10, time.Now(), time.Now().AddDate(0, 0, 1))
	assert.ErrorIs(t, err, models.ErrInvalidUserID)
}

func TestNewBooking_EndDateBeforeStartDate(t *testing.T) {
	start := time.Now().AddDate(0, 0, 7)
	end := time.Now().AddDate(0, 0, 1)
	_, err := models.NewBooking(1, 10, start, end)
	assert.ErrorIs(t, err, models.ErrEndDateBeforeStartDate)
}

func TestConfirm_FromAwaitsConfirmation(t *testing.T) {
	booking := createTestBooking(t)

	err := booking.Confirm()

	require.NoError(t, err)
	assert.Equal(t, models.BookingStatusConfirmed, booking.Status())
}

func TestConfirm_FromConfirmed_Error(t *testing.T) {
	booking := createTestBooking(t)
	_ = booking.Confirm()

	err := booking.Confirm()

	assert.ErrorIs(t, err, models.ErrInvalidStatusTransition)
}

func TestStartCancellation_FromAwaitsConfirmation(t *testing.T) {
	booking := createTestBooking(t)
	now := time.Now()

	err := booking.StartCancellation(now, now)

	require.NoError(t, err)
	assert.Equal(t, models.BookingStatusCancellationPending, booking.Status())
	require.NotNil(t, booking.PreviousStatus())
	assert.Equal(t, models.BookingStatusAwaitsConfirmation, *booking.PreviousStatus())
	require.NotNil(t, booking.CancellationRequestedAt())
}

func TestStartCancellation_FromConfirmed_FutureStartDate(t *testing.T) {
	booking := createTestBooking(t)
	_ = booking.Confirm()
	now := time.Now()

	err := booking.StartCancellation(now, now)

	require.NoError(t, err)
	assert.Equal(t, models.BookingStatusCancellationPending, booking.Status())
	require.NotNil(t, booking.PreviousStatus())
	assert.Equal(t, models.BookingStatusConfirmed, *booking.PreviousStatus())
}

func TestStartCancellation_FromConfirmed_PastStartDate_Error(t *testing.T) {
	b := models.RestoreBooking(
		1,
		models.BookingStatusConfirmed,
		1, 10,
		time.Now().AddDate(0, 0, -3),
		time.Now().AddDate(0, 0, -1),
		time.Now().AddDate(0, 0, -5),
		nil, nil,
	)

	err := b.StartCancellation(time.Now(), time.Now())

	assert.ErrorIs(t, err, models.ErrCannotCancelPastBooking)
}

func TestStartCancellation_FromCancellationPending_Error(t *testing.T) {
	booking := createTestBooking(t)
	now := time.Now()
	require.NoError(t, booking.StartCancellation(now, now))

	err := booking.StartCancellation(now, now)

	assert.ErrorIs(t, err, models.ErrInvalidStatusTransition)
}

func TestStartCancellation_FromCancelled_Error(t *testing.T) {
	booking := createTestBooking(t)
	now := time.Now()
	require.NoError(t, booking.StartCancellation(now, now))
	require.NoError(t, booking.CompleteCancel())

	err := booking.StartCancellation(now, now)

	assert.ErrorIs(t, err, models.ErrInvalidStatusTransition)
}

func TestCompleteCancel_FromCancellationPending(t *testing.T) {
	booking := createTestBooking(t)
	now := time.Now()
	require.NoError(t, booking.StartCancellation(now, now))

	err := booking.CompleteCancel()

	require.NoError(t, err)
	assert.Equal(t, models.BookingStatusCancelled, booking.Status())
	assert.Nil(t, booking.PreviousStatus())
	assert.Nil(t, booking.CancellationRequestedAt())
}

func TestCompleteCancel_FromAwaitsConfirmation_Error(t *testing.T) {
	booking := createTestBooking(t)

	err := booking.CompleteCancel()

	assert.ErrorIs(t, err, models.ErrInvalidStatusTransition)
}

func TestRollbackCancellation_RestoresAwaitsConfirmation(t *testing.T) {
	booking := createTestBooking(t)
	now := time.Now()
	require.NoError(t, booking.StartCancellation(now, now))

	err := booking.RollbackCancellation()

	require.NoError(t, err)
	assert.Equal(t, models.BookingStatusAwaitsConfirmation, booking.Status())
	assert.Nil(t, booking.PreviousStatus())
	assert.Nil(t, booking.CancellationRequestedAt())
}

func TestRollbackCancellation_RestoresConfirmed(t *testing.T) {
	booking := createTestBooking(t)
	require.NoError(t, booking.Confirm())
	now := time.Now()
	require.NoError(t, booking.StartCancellation(now, now))

	err := booking.RollbackCancellation()

	require.NoError(t, err)
	assert.Equal(t, models.BookingStatusConfirmed, booking.Status())
}

func TestRollbackCancellation_FromAwaitsConfirmation_Error(t *testing.T) {
	booking := createTestBooking(t)

	err := booking.RollbackCancellation()

	assert.ErrorIs(t, err, models.ErrInvalidStatusTransition)
}

func TestRollbackCancellation_AfterComplete_Error(t *testing.T) {
	booking := createTestBooking(t)
	now := time.Now()
	require.NoError(t, booking.StartCancellation(now, now))
	require.NoError(t, booking.CompleteCancel())

	err := booking.RollbackCancellation()

	assert.ErrorIs(t, err, models.ErrInvalidStatusTransition)
}

func createTestBooking(t *testing.T) *models.Booking {
	t.Helper()
	b, err := models.NewBooking(1, 10, time.Now().AddDate(0, 0, 7), time.Now().AddDate(0, 0, 14))
	require.NoError(t, err)
	return b
}
