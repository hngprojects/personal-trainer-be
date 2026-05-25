package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
)

func ptr[T any](v T) *T { return &v }

func (s *routerImpl) GetSubscriptionPlans(c *gin.Context) {
	plans := []api.SubscriptionPlan{
		{
			Id:               "casual",
			Name:             "The Casual",
			SessionsPerMonth: 1,
			Amount:           2000,
			Currency:         "USD",
			AmountDisplay:    "$20/month",
			TrialDays:        7,
			AppleProductId:   "com.fitcal.plan.casual.monthly",
			GoogleProductId:  "fitcal_plan_casual_monthly",
			Features: []string{
				"1 guided session",
				"Expert guidance during sessions",
				"Workout reminders",
			},
		},
		{
			Id:               "committed",
			Name:             "The Committed",
			Tag:              ptr("Most Popular"),
			SessionsPerMonth: 12,
			Amount:           8000,
			Currency:         "USD",
			AmountDisplay:    "$80/month",
			TrialDays:        7,
			AppleProductId:   "com.fitcal.plan.committed.monthly",
			GoogleProductId:  "fitcal_plan_committed_monthly",
			Features: []string{
				"12 guided sessions per month",
				"Session duration: 60 minutes",
				"Trainer calls you at scheduled time",
				"Expert guidance during sessions",
				"Workout reminders",
			},
		},
		{
			Id:               "consistent",
			Name:             "The Consistent",
			SessionsPerMonth: 18,
			Amount:           12000,
			Currency:         "USD",
			AmountDisplay:    "$120/month",
			TrialDays:        7,
			AppleProductId:   "com.fitcal.plan.consistent.monthly",
			GoogleProductId:  "fitcal_plan_consistent_monthly",
			Features: []string{
				"18 guided sessions per month",
				"Expert guidance during sessions",
				"Workout reminders",
				"Meal recommendations",
			},
		},
	}

	c.JSON(http.StatusOK, api.SubscriptionPlansResponse{
		Code:    api.CodeOK,
		Message: "PLANS_FETCHED",
		Data:    plans,
	})
}
