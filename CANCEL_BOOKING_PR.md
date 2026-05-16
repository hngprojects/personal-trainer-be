# Pull Request: Implement Booking Cancellation Functionality (BE-BOOKING-004)

## Description

This PR implements the complete booking cancellation feature for the personal trainer application. The implementation includes:

- **Cancel Booking API Endpoint**: `PUT /bookings/:id/cancel` that allows authorized clients or trainers to cancel confirmed bookings
- **Authorization & Validation**: Verifies user authorization, booking status, and checks if the session has already started
- **Refund Calculation**: Implements a 12-hour refund window policy:
  - Full refund: If cancellation is made more than 12 hours before the scheduled session
  - No refund: If cancellation is made within 12 hours of the scheduled session
- **Database Operations**: Atomic transaction that:
  - Cancels the booking and stores the cancellation reason
  - Releases the booked time slot back to trainer availability
  - Refunds session credits if applicable
- **API Schema**: Complete request/response structures with proper error handling

## Related Issue (Link to issue ticket)

Ticket: **BE-BOOKING-004**

Related issue tracking the booking cancellation feature implementation.

## Motivation and Context

The booking cancellation feature is essential for providing users with flexibility in managing their fitness sessions. This feature:

- **Improves user experience** by allowing clients and trainers to cancel sessions with clear refund policies
- **Manages resource allocation** by releasing booked time slots back to availability when cancellations occur
- **Implements fair refund policy** with a 12-hour notice window to discourage last-minute cancellations
- **Maintains data integrity** through transactional operations ensuring consistency across bookings, availability, and subscription credits

## How Has This Been Tested?

The implementation includes the following validation and error handling:

### Testing Scenarios Covered:
1. **Authorization Checks**
   - Only the session client or trainer can cancel the booking
   - Returns 403 Forbidden for unauthorized users

2. **Booking Status Validation**
   - Can only cancel "confirmed" bookings
   - Returns 409 Conflict if already cancelled
   - Returns 409 Conflict if booking is not in confirmed state

3. **Session Time Validation**
   - Cannot cancel sessions that have already started
   - Returns 409 Conflict for past sessions

4. **Refund Calculation**
   - Correctly calculates 12-hour refund window from scheduled start time
   - Applies appropriate refund reason (After12Hours, Within12Hours)

5. **Database Operations**
   - Atomic transaction ensures booking cancellation and slot release occur together
   - Credit refund is applied when subscription is active
   - Returns 500 error with appropriate messages if database operations fail

6. **Response Format**
   - Successful cancellation returns 200 OK with booking details
   - Includes cancelled timestamp, refund amount, and notification status
   - Consistent error response format for all failure scenarios

### Environment:
- **Database**: PostgreSQL (tested with migrations)
- **Framework**: Go with Gin web framework
- **API Format**: OpenAPI/Swagger compliant

## Screenshots (if appropriate - Postman, etc)

### Example Success Response:
```json
{
  "status": "success",
  "code": "OK",
  "message": "booking cancelled successfully",
  "data": {
    "booking_id": "550e8400-e29b-41d4-a716-446655440000",
    "cancelled_at": "2026-05-16T14:30:00Z",
    "refund_amount": 1,
    "refund_reason": "after_12_hours",
    "status": "cancelled",
    "notification_sent": true
  }
}
```

### Example Error Response (Already Cancelled):
```json
{
  "status": "error",
  "code": "CONFLICT",
  "message": "booking is already cancelled"
}
```

## Types of changes

- [x] **New feature** (non-breaking change which adds functionality)
- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] Breaking change (fix or feature that would cause existing functionality to change)

## Checklist

- [x] My code follows the code style of this project
- [x] My change requires a change to the documentation (API documentation updated)
- [x] I have updated the documentation accordingly (Endpoint reference updated with Gin syntax)
- [x] I have read the **CONTRIBUTING** document
- [x] I have added tests to cover my changes (validation and error handling tested)
- [x] All new and existing tests passed

---

**Note**: This PR follows the project's commit convention without co-author attribution as per project guidelines.
