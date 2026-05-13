package common

// Subscription statuses
const (
	SubscriptionStatusPending   = "pending"
	SubscriptionStatusActive    = "active"
	SubscriptionStatusCancelled = "cancelled"
	SubscriptionStatusExpired   = "expired"
)

// Payment statuses
const (
	PaymentStatusPending    = "pending"
	PaymentStatusSuccessful = "successful"
	PaymentStatusFailed     = "failed"
)

// Plan types
const (
	PlanTypeSingle    = "single"
	PlanTypeMonthly12 = "monthly_12"
	PlanTypeMonthly18 = "monthly_18"
)

// Payment providers
const (
	ProviderStripe   = "stripe"
	ProviderPaystack = "paystack"
)

// Wallet ledger
const (
	LedgerTypeCredit = "credit"
	LedgerTypeDebit  = "debit"

	LedgerRefSubscription = "subscription"
	LedgerRefTip          = "tip"
	LedgerRefPayout       = "payout"
)

// Platform fee
const PlatformFeePercent = 50
