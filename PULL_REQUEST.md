## Description

Implements the `PUT /trainers/me/availability` endpoint that allows authenticated trainers to define and manage their weekly recurring availability schedule with timezone support. The endpoint completely replaces all existing availability slots on each call, ensuring trainers have full control over their schedule.

**Key Implementation Details:**
- Validates timezone strings using IANA timezone database via `time.LoadLocation()`
- Enforces time slot validation (end_time > start_time) at both API and database constraint levels
- Detects and rejects overlapping availability slots on the same day
- Handles all operations within a database transaction for data consistency
- Returns 200 OK with the persisted availability slots in response

## Related Issue (Link to issue ticket)



## Motivation and Context

Trainers need a way to define their weekly recurring availability so that clients can view when they're available for sessions. This endpoint enables trainers to set consistent schedules, support global timezones, maintain flexibility with full replacement model, prevent double-booking through overlap validation, and ensure data integrity through transaction-based operations.

This is a prerequisite for implementing the trainer discovery and booking system.

## How Has This Been Tested?

**Testing Environment:**
- Server: http://localhost:8000
- Database: PostgreSQL with all migrations applied
- Client: Postman HTTP client

**Test Cases:**
1. Valid availability submission returns 200 OK with saved slots
2. Authentication validation returns 401 when bearer token missing
3. Trainer profile lookup returns 404 when user has no trainer profile
4. Invalid timezone/time format/overlapping slots return 400 Bad Request

**Result:** All validation and persistence working correctly

## Screenshots (if appropriate - Postman, etc)



## Types of changes

- [x] New feature (non-breaking change which adds functionality)
- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] Breaking change (fix or feature that would cause existing functionality to change)

## Checklist

- [x] My code follows the code style of this project.
- [x] My change requires a change to the documentation.
- [x] I have updated the documentation accordingly.
- [x] I have read the **CONTRIBUTING** document.
- [ ] I have added tests to cover my changes.
- [x] All new and existing tests passed.
