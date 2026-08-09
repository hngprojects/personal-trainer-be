package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

func main() {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		log.Fatal("RESEND_API_KEY env var is required")
	}
	from := os.Getenv("RESEND_FROM")
	if from == "" {
		from = "fitcal@hng14.com"
	}
	clientEmail := os.Getenv("TEST_CLIENT_EMAIL")
	if clientEmail == "" {
		log.Fatal("TEST_CLIENT_EMAIL env var is required")
	}
	trainerEmail := os.Getenv("TEST_TRAINER_EMAIL")
	if trainerEmail == "" {
		log.Fatal("TEST_TRAINER_EMAIL env var is required")
	}
	testPhone := os.Getenv("TEST_PHONE")
	if testPhone == "" {
		testPhone = "+10000000000"
	}

	mailer := email.NewResendMailer(apiKey, from, "")

	sessionTime := time.Now().Add(48 * time.Hour).UTC()
	sessionEnd := sessionTime.Add(time.Hour)

	// 1. Client booking confirmation (FaceTime)
	err := mailer.SendBookingConfirmation(
		clientEmail,
		"Test Client",
		"Test Trainer",
		sessionTime,
		sessionEnd,
		"Africa/Lagos",
		"imessage",
		"",
		"",
		testPhone,
		"test-booking-id-001",
		false,
	)
	if err != nil {
		log.Printf("client booking email failed: %v", err)
	} else {
		fmt.Println("✓ Client booking confirmation sent to", clientEmail)
	}

	// 2. Trainer booking confirmation (FaceTime)
	err = mailer.SendBookingConfirmation(
		trainerEmail,
		"Test Client",
		"Test Trainer",
		sessionTime,
		sessionEnd,
		"Africa/Lagos",
		"imessage",
		"",
		"",
		testPhone,
		"test-booking-id-001",
		true,
	)
	if err != nil {
		log.Printf("trainer booking email failed: %v", err)
	} else {
		fmt.Println("✓ Trainer booking confirmation sent to", trainerEmail)
	}

	// 3. Trainer reschedule notification
	newTime := sessionTime.Add(2 * time.Hour)
	err = mailer.SendPaidSessionRescheduleTrainerNotification(
		trainerEmail,
		"Test Trainer",
		"Test Client",
		newTime,
		60,
		"Africa/Lagos",
		"imessage",
		testPhone,
		"",
	)
	if err != nil {
		log.Printf("trainer reschedule email failed: %v", err)
	} else {
		fmt.Println("✓ Trainer reschedule notification sent to", trainerEmail)
	}

	// 4. Client reschedule confirmation
	err = mailer.SendPaidSessionRescheduleConfirmation(
		clientEmail,
		"Test Client",
		newTime,
		60,
		"Africa/Lagos",
		"imessage",
		"",
		testPhone,
		"",
	)
	if err != nil {
		log.Printf("client reschedule email failed: %v", err)
	} else {
		fmt.Println("✓ Client reschedule confirmation sent to", clientEmail)
	}
}
