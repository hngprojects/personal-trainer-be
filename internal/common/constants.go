package common

const PlatformFeePercent = 50

const (
	PlanTypeSingle    = "single"
	PlanTypeMonthly12 = "monthly_12"
	PlanTypeMonthly18 = "monthly_18"
)

const (
	ProviderStripe   = "stripe"
	ProviderPaystack = "paystack"
)

const (
	LedgerTypeCredit     = "credit"
	LedgerRefSubscription = "subscription"
	LedgerRefTip         = "tip"
)

const (
	PaymentStatusPending    = "pending"
	PaymentStatusSuccessful = "successful"
	PaymentStatusFailed     = "failed"
)

const (
	SubscriptionStatusActive    = "active"
	SubscriptionStatusCancelled = "cancelled"
)
