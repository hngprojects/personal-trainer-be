# Technical Product Requirements

## African Personal Trainer Platform

**Version:** 1.0  
**Date:** May 2026  
**Status:** Draft

---

## 1. Product Overview

The African Personal Trainer Platform connects top Nigerian personal trainers with clients in the US and UK. The platform enables trainers to offer guided fitness sessions, manage client progress, and receive payments, while clients can discover trainers, book sessions, and track their fitness journey.

---

## 2. Target Users

**Trainers**

- Nigerian-based certified personal trainers
- Looking to expand their client base internationally

**Clients**

- Fitness enthusiasts in the US and UK
- Looking for affordable, quality personal training online

---

## 3. Platform Targets

- Mobile application (iOS and Android)
- Web landing page

---

## 4. Core Features

### 4.1 Authentication

- Email and password registration with email verification
- Google OAuth sign in and sign up
- Forgot password and reset password
- Change password
- Session management

### 4.2 Trainer Discovery

- Browse and search for trainers
- View trainer profiles, specialties, and ratings
- Filter trainers by category and availability

### 4.3 Booking & Sessions

- Book a session with a trainer
- View upcoming and past sessions
- Session history and details

### 4.4 Workout Plans

- Trainers create and assign workout plans to clients
- Clients view and track assigned plans

### 4.5 Progress Tracking

- Clients log workout metrics and performance
- Progress charts and analytics over time
- Personal bests and milestone tracking

### 4.6 Subscriptions & Payments

- Subscription plans (e.g. The Committed — $80/month, The Casual — $15/session)
- Plan upgrade and downgrade
- USDC crypto payouts for trainers
- Payment history

### 4.7 User Profile & Account Settings

- Edit personal information
- Change email and password
- Linked accounts (Google, Apple)
- Two-factor authentication
- Active sessions and devices
- Account deactivation

### 4.8 Notifications & Reminders

- Session reminders
- Progress updates
- Subscription alerts

---

## 5. Technical Stack

### Backend

| Service                 | Technology                       |
| ----------------------- | -------------------------------- |
| Primary API             | Go (Golang)                      |
| Performance & Analytics | Rust                             |
| Database                | PostgreSQL                       |
| Authentication          | Session-based with secure tokens |
| Email                   | SMTP                             |
| Password Hashing        | bcrypt                           |

### Frontend

| Layer            | Technology                   |
| ---------------- | ---------------------------- |
| Mobile App       | TBD (React Native / Flutter) |
| Web Landing Page | TBD (Next.js / React)        |

### Infrastructure

| Component        | Tool                         |
| ---------------- | ---------------------------- |
| Hosting          | Render                       |
| Database Hosting | Supabase / Render PostgreSQL |
| File Storage     | Cloudinary / AWS S3          |
| CI/CD            | GitHub Actions               |

---

## 6. Architecture Overview

```
Mobile App / Web
      ↓
   API Gateway
      ↓
Go Service (Auth, Users, Booking, Plans)
      ↓
Rust Service (Analytics, Progress, Reports)
      ↓
PostgreSQL Database
```

---

## 7. API Standards

- RESTful JSON API
- Base URL: `api.trainerapp.com/v1`
- All routes prefixed by resource (e.g. `/auth`, `/users`, `/sessions`)
- Standard HTTP status codes
- Consistent response format:

**Success:**

```json
{
  "status": "success",
  "message": "Human-readable message",
  "data": {},
  "meta": {}
}
```

**Error:**

```json
{
  "status": "error",
  "message": "Human-readable error message",
  "errors": []
}
```

---

## 8. Database Schema (Core Tables)

| Table                | Purpose                                         |
| -------------------- | ----------------------------------------------- |
| `users`              | Stores all user accounts (trainers and clients) |
| `sessions`           | Auth session management                         |
| `verification_codes` | Email verification during sign up               |
| `trainer_profiles`   | Trainer-specific information                    |
| `bookings`           | Session bookings between trainers and clients   |
| `workout_plans`      | Trainer-created plans                           |
| `progress_logs`      | Client workout tracking                         |
| `subscriptions`      | Client subscription and payment records         |

---

## 9. Security Requirements

- Passwords are hashed using **bcrypt** (cost factor ≥ 12)
- Authentication uses **access and refresh tokens (JWT-based)**
- Access tokens are short-lived; refresh tokens are used to obtain new access tokens
- All tokens are **cryptographically signed and secure**
- Revoked or invalidated tokens are stored in a **cache layer** (e.g. Redis) and checked on each request
- Session lifetime is controlled via token expiration (access + refresh flow)
- Verification and password reset codes expire after **10 minutes**
- HTTPS is enforced in production
- CORS is explicitly configured (no wildcard origins in production)
- Rate limiting is applied on all authentication routes
- No plaintext passwords are ever stored or logged
- Sensitive data (tokens, credentials) are never exposed in API responses or logs

## 10. Non-Functional Requirements

| Requirement       | Target                                      |
| ----------------- | ------------------------------------------- |
| API Response Time | < 300ms for standard requests               |
| Uptime            | 99.5%                                       |
| Mobile Support    | iOS and Android                             |
| Data Privacy      | GDPR compliant (for UK users)               |
| Scalability       | Horizontal scaling via stateless API design |

---

## 11. Milestones

| Milestone | Description                                            |
| --------- | ------------------------------------------------------ |
| v0.1      | Authentication module complete (sign up, login, OAuth) |
| v0.2      | Trainer discovery and profiles                         |
| v0.3      | Booking and session management                         |
| v0.4      | Progress tracking and analytics                        |
| v0.5      | Payments and subscriptions                             |
| v1.0      | Full platform launch                                   |
