//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPR12ModelPricingResolver_ChannelOneHourPriceOverridesInheritedBase verifies
// that a channel's explicit 1h cache-write price reaches the resolved base pricing
// and is used by the split cache-creation calculation, even when the other token
// prices are inherited from the model catalog.
func TestPR12ModelPricingResolver_ChannelOneHourPriceOverridesInheritedBase(t *testing.T) {
	cacheWrite5m := 4e-6
	cacheWrite1h := 21e-6
	resolver := newResolverWithChannel(t, []ChannelModelPricing{{
		Platform:          PlatformAnthropic,
		Models:            []string{"claude-sonnet-4"},
		BillingMode:       BillingModeToken,
		CacheWritePrice:   &cacheWrite5m,
		CacheWrite1hPrice: &cacheWrite1h,
	}})

	resolved := resolver.Resolve(context.Background(), PricingInput{
		Model:   "claude-sonnet-4",
		GroupID: groupIDPtr(),
	})

	require.NotNil(t, resolved)
	require.Equal(t, PricingSourceChannel, resolved.Source)
	require.NotNil(t, resolved.BasePricing)
	require.True(t, resolved.SupportsCacheBreakdown)
	require.True(t, resolved.BasePricing.SupportsCacheBreakdown)
	// Input/output and the channel's 5m price are present as usual; the 1h
	// value must not remain at the inherited catalog value (or zero).
	require.InDelta(t, 3e-6, resolved.BasePricing.InputPricePerToken, 1e-12)
	require.InDelta(t, cacheWrite5m, resolved.BasePricing.CacheCreation5mPrice, 1e-12)
	require.InDelta(t, cacheWrite1h, resolved.BasePricing.CacheCreation1hPrice, 1e-12)

	cost, err := resolver.billingService.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "claude-sonnet-4",
		GroupID:        groupIDPtr(),
		Tokens:         UsageTokens{CacheCreationTokens: 5, CacheCreation5mTokens: 2, CacheCreation1hTokens: 3},
		RateMultiplier: 1,
		Resolver:       resolver,
		Resolved:       resolved,
	})
	require.NoError(t, err)
	require.InDelta(t, 2*cacheWrite5m+3*cacheWrite1h, cost.CacheCreationCost, 1e-12)
}

// TestPR12RecordUsage_FreeFastMissingPricingStillPersists verifies that the
// FreeFast Standard-tier re-calculation treats unavailable pricing like the
// initial calculation: it records a zero-cost usage row instead of dropping it.
func TestPR12RecordUsage_FreeFastMissingPricingStillPersists(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, userRepo, subRepo, nil)

	groupID := int64(12012)
	serviceTier := "priority"
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:   "pr12_free_fast_missing_pricing",
			Model:       "pr12-model-without-pricing",
			ServiceTier: &serviceTier,
			Usage:       OpenAIUsage{InputTokens: 1200, OutputTokens: 300},
			Duration:    time.Second,
		},
		APIKey: &APIKey{
			ID:      12012,
			GroupID: &groupID,
			Group: &Group{
				ID:             groupID,
				Platform:       PlatformOpenAI,
				RateMultiplier: 1,
				FreeOpenAIFast: true,
			},
		},
		User:    &User{ID: 12012},
		Account: &Account{ID: 12012, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	})

	require.NoError(t, err)
	require.Equal(t, 1, billingRepo.calls)
	require.Equal(t, 1, usageRepo.calls)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "pr12-model-without-pricing", usageRepo.lastLog.Model)
	require.Equal(t, 1200, usageRepo.lastLog.InputTokens)
	require.Equal(t, 300, usageRepo.lastLog.OutputTokens)
	require.Zero(t, usageRepo.lastLog.TotalCost)
	require.Zero(t, usageRepo.lastLog.ActualCost)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeToken), *usageRepo.lastLog.BillingMode)
}
