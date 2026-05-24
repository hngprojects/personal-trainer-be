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
			Id:               ptr("casual"),
			Name:             ptr("The Casual"),
			SessionsPerMonth: ptr(1),
			Amount:           ptr(2000),
			Currency:         ptr("USD"),
			AmountDisplay:    ptr("$20/month"),
			TrialDays:        ptr(7),
			AppleProductId:   ptr("com.fitcal.plan.casual.monthly"),
			GoogleProductId:  ptr("fitcal_plan_casual_monthly"),
			Features: ptr([]string{
				"1 guided session",
				"Trained expert guidance",
				"Workout reminder",
			}),
		},
		{
			Id:               ptr("committed"),
			Name:             ptr("The Committed"),
			Tag:              ptr("Most Popular"),
			SessionsPerMonth: ptr(12),
			Amount:           ptr(8000),
			Currency:         ptr("USD"),
			AmountDisplay:    ptr("$80/month"),
			TrialDays:        ptr(7),
			AppleProductId:   ptr("com.fitcal.plan.committed.monthly"),
			GoogleProductId:  ptr("fitcal_plan_committed_monthly"),
			Features: ptr([]string{
				"12 guided sessions per month",
				"Each session is 60 minutes",
				"Trainer will call you at scheduled time",
				"Personalized guidance during sessions",
			}),
		},
		{
			Id:               ptr("consistent"),
			Name:             ptr("The Consistent"),
			SessionsPerMonth: ptr(18),
			Amount:           ptr(12000),
			Currency:         ptr("USD"),
			AmountDisplay:    ptr("$120/month"),
			TrialDays:        ptr(7),
			AppleProductId:   ptr("com.fitcal.plan.consistent.monthly"),
			GoogleProductId:  ptr("fitcal_plan_consistent_monthly"),
			Features: ptr([]string{
				"18 guided sessions per month",
				"Trained expert guidance",
				"Workout reminder",
				"Cancel anytime",
				"Meal recommendations",
			}),
		},
	}

	c.JSON(http.StatusOK, api.SubscriptionPlansResponse{
		Code:    string(api.CodeOK),
		Message: "PLANS_FETCHED",
		Status:  api.SubscriptionPlansResponseStatusSuccess,
		Data:    &plans,
	})
}
