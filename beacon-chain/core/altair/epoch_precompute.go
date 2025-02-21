package altair

import (
	"context"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/beacon-chain/core"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/epoch/precompute"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.opencensus.io/trace"
)

// InitializeEpochValidators gets called at the beginning of process epoch cycle to return
// pre computed instances of validators attesting records and total
// balances attested in an epoch.
func InitializeEpochValidators(ctx context.Context, st state.BeaconStateAltair) ([]*precompute.Validator, *precompute.Balance, error) {
	_, span := trace.StartSpan(ctx, "altair.InitializeEpochValidators")
	defer span.End()
	pValidators := make([]*precompute.Validator, st.NumValidators())
	bal := &precompute.Balance{}
	prevEpoch := core.PrevEpoch(st)
	currentEpoch := core.CurrentEpoch(st)
	inactivityScores, err := st.InactivityScores()
	if err != nil {
		return nil, nil, err
	}

	// This shouldn't happen with a correct beacon state,
	// but rather be safe to defend against index out of bound panics.
	if st.NumValidators() > len(inactivityScores) {
		return nil, nil, errors.New("num of validators can't be greater than length of inactivity scores")
	}
	if err := st.ReadFromEveryValidator(func(idx int, val state.ReadOnlyValidator) error {
		// Was validator withdrawable or slashed
		withdrawable := currentEpoch >= val.WithdrawableEpoch()
		pVal := &precompute.Validator{
			IsSlashed:                    val.Slashed(),
			IsWithdrawableCurrentEpoch:   withdrawable,
			CurrentEpochEffectiveBalance: val.EffectiveBalance(),
			InactivityScore:              inactivityScores[idx],
		}
		// Validator active current epoch
		if helpers.IsActiveValidatorUsingTrie(val, currentEpoch) {
			pVal.IsActiveCurrentEpoch = true
			bal.ActiveCurrentEpoch += val.EffectiveBalance()
		}
		// Validator active previous epoch
		if helpers.IsActiveValidatorUsingTrie(val, prevEpoch) {
			pVal.IsActivePrevEpoch = true
			bal.ActivePrevEpoch += val.EffectiveBalance()
		}

		pValidators[idx] = pVal
		return nil
	}); err != nil {
		return nil, nil, errors.Wrap(err, "could not initialize epoch validator")
	}
	return pValidators, bal, nil
}

// ProcessInactivityScores of beacon chain. This updates inactivity scores of beacon chain and
// updates the precompute validator struct for later processing. The inactivity scores work as following:
// For fully inactive validators and perfect active validators, the effect is the same as before Altair.
// For a validator is inactive and the chain fails to finalize, the inactivity score increases by a fixed number, the total loss after N epochs is proportional to N**2/2.
// For imperfectly active validators. The inactivity score's behavior is specified by this function:
//    If a validator fails to submit an attestation with the correct target, their inactivity score goes up by 4.
//    If they successfully submit an attestation with the correct source and target, their inactivity score drops by 1
//    If the chain has recently finalized, each validator's score drops by 16.
func ProcessInactivityScores(
	ctx context.Context,
	state state.BeaconState,
	vals []*precompute.Validator,
) (state.BeaconState, []*precompute.Validator, error) {
	_, span := trace.StartSpan(ctx, "altair.ProcessInactivityScores")
	defer span.End()

	cfg := params.BeaconConfig()
	if core.CurrentEpoch(state) == cfg.GenesisEpoch {
		return state, vals, nil
	}

	inactivityScores, err := state.InactivityScores()
	if err != nil {
		return nil, nil, err
	}

	bias := cfg.InactivityScoreBias
	recoveryRate := cfg.InactivityScoreRecoveryRate
	prevEpoch := core.PrevEpoch(state)
	finalizedEpoch := state.FinalizedCheckpointEpoch()
	for i, v := range vals {
		if !precompute.EligibleForRewards(v) {
			continue
		}

		if v.IsPrevEpochTargetAttester && !v.IsSlashed {
			// Decrease inactivity score when validator gets target correct.
			if v.InactivityScore > 0 {
				v.InactivityScore -= 1
			}
		} else {
			v.InactivityScore += bias
		}

		if !helpers.IsInInactivityLeak(prevEpoch, finalizedEpoch) {
			score := recoveryRate
			// Prevents underflow below 0.
			if score > v.InactivityScore {
				score = v.InactivityScore
			}
			v.InactivityScore -= score
		}
		inactivityScores[i] = v.InactivityScore
	}

	if err := state.SetInactivityScores(inactivityScores); err != nil {
		return nil, nil, err
	}

	return state, vals, nil
}

// ProcessEpochParticipation processes the epoch participation in state and updates individual validator's pre computes,
// it also tracks and updates epoch attesting balances.
// Spec code:
// if epoch == get_current_epoch(state):
//        epoch_participation = state.current_epoch_participation
//    else:
//        epoch_participation = state.previous_epoch_participation
//    active_validator_indices = get_active_validator_indices(state, epoch)
//    participating_indices = [i for i in active_validator_indices if has_flag(epoch_participation[i], flag_index)]
//    return set(filter(lambda index: not state.validators[index].slashed, participating_indices))
func ProcessEpochParticipation(
	ctx context.Context,
	state state.BeaconState,
	bal *precompute.Balance,
	vals []*precompute.Validator,
) ([]*precompute.Validator, *precompute.Balance, error) {
	_, span := trace.StartSpan(ctx, "altair.ProcessEpochParticipation")
	defer span.End()

	cp, err := state.CurrentEpochParticipation()
	if err != nil {
		return nil, nil, err
	}
	cfg := params.BeaconConfig()
	targetIdx := cfg.TimelyTargetFlagIndex
	sourceIdx := cfg.TimelySourceFlagIndex
	headIdx := cfg.TimelyHeadFlagIndex
	for i, b := range cp {
		if HasValidatorFlag(b, targetIdx) && vals[i].IsActiveCurrentEpoch {
			vals[i].IsCurrentEpochTargetAttester = true
		}
	}
	pp, err := state.PreviousEpochParticipation()
	if err != nil {
		return nil, nil, err
	}
	for i, b := range pp {
		if HasValidatorFlag(b, sourceIdx) && vals[i].IsActivePrevEpoch {
			vals[i].IsPrevEpochAttester = true
		}
		if HasValidatorFlag(b, targetIdx) && vals[i].IsActivePrevEpoch {
			vals[i].IsPrevEpochTargetAttester = true
		}
		if HasValidatorFlag(b, headIdx) && vals[i].IsActivePrevEpoch {
			vals[i].IsPrevEpochHeadAttester = true
		}
	}
	bal = precompute.UpdateBalance(vals, bal)
	return vals, bal, nil
}

// ProcessRewardsAndPenaltiesPrecompute processes the rewards and penalties of individual validator.
// This is an optimized version by passing in precomputed validator attesting records and and total epoch balances.
func ProcessRewardsAndPenaltiesPrecompute(
	state state.BeaconStateAltair,
	bal *precompute.Balance,
	vals []*precompute.Validator,
) (state.BeaconStateAltair, error) {
	// Don't process rewards and penalties in genesis epoch.
	if core.CurrentEpoch(state) == 0 {
		return state, nil
	}

	numOfVals := state.NumValidators()
	// Guard against an out-of-bounds using validator balance precompute.
	if len(vals) != numOfVals || len(vals) != state.BalancesLength() {
		return state, errors.New("validator registries not the same length as state's validator registries")
	}

	attsRewards, attsPenalties, err := AttestationsDelta(state, bal, vals)
	if err != nil {
		return nil, errors.Wrap(err, "could not get attestation delta")
	}

	balances := state.Balances()
	for i := 0; i < numOfVals; i++ {
		vals[i].BeforeEpochTransitionBalance = balances[i]

		// Compute the post balance of the validator after accounting for the
		// attester and proposer rewards and penalties.
		balances[i] = helpers.IncreaseBalanceWithVal(balances[i], attsRewards[i])
		balances[i] = helpers.DecreaseBalanceWithVal(balances[i], attsPenalties[i])

		vals[i].AfterEpochTransitionBalance = balances[i]
	}

	if err := state.SetBalances(balances); err != nil {
		return nil, errors.Wrap(err, "could not set validator balances")
	}

	return state, nil
}

// AttestationsDelta computes and returns the rewards and penalties differences for individual validators based on the
// voting records.
func AttestationsDelta(state state.BeaconStateAltair, bal *precompute.Balance, vals []*precompute.Validator) (rewards, penalties []uint64, err error) {
	numOfVals := state.NumValidators()
	rewards = make([]uint64, numOfVals)
	penalties = make([]uint64, numOfVals)
	prevEpoch := core.PrevEpoch(state)
	finalizedEpoch := state.FinalizedCheckpointEpoch()

	cfg := params.BeaconConfig()
	increment := cfg.EffectiveBalanceIncrement
	factor := cfg.BaseRewardFactor
	baseRewardMultiplier := increment * factor / mathutil.IntegerSquareRoot(bal.ActiveCurrentEpoch)
	leak := helpers.IsInInactivityLeak(prevEpoch, finalizedEpoch)
	inactivityDenominator := cfg.InactivityScoreBias * cfg.InactivityPenaltyQuotientAltair

	for i, v := range vals {
		rewards[i], penalties[i] = attestationDelta(bal, v, baseRewardMultiplier, inactivityDenominator, leak)
	}

	return rewards, penalties, nil
}

func attestationDelta(
	bal *precompute.Balance,
	v *precompute.Validator,
	baseRewardMultiplier, inactivityDenominator uint64,
	inactivityLeak bool) (reward, penalty uint64) {
	eligible := v.IsActivePrevEpoch || (v.IsSlashed && !v.IsWithdrawableCurrentEpoch)
	// Per spec `ActiveCurrentEpoch` can't be 0 to process attestation delta.
	if !eligible || bal.ActiveCurrentEpoch == 0 {
		return 0, 0
	}

	cfg := params.BeaconConfig()
	increment := cfg.EffectiveBalanceIncrement
	effectiveBalance := v.CurrentEpochEffectiveBalance
	baseReward := (effectiveBalance / increment) * baseRewardMultiplier
	activeIncrement := bal.ActiveCurrentEpoch / increment

	weightDenominator := cfg.WeightDenominator
	srcWeight := cfg.TimelySourceWeight
	tgtWeight := cfg.TimelyTargetWeight
	headWeight := cfg.TimelyHeadWeight
	reward, penalty = uint64(0), uint64(0)
	// Process source reward / penalty
	if v.IsPrevEpochAttester && !v.IsSlashed {
		if !inactivityLeak {
			n := baseReward * srcWeight * (bal.PrevEpochAttested / increment)
			reward += n / (activeIncrement * weightDenominator)
		}
	} else {
		penalty += baseReward * srcWeight / weightDenominator
	}

	// Process target reward / penalty
	if v.IsPrevEpochTargetAttester && !v.IsSlashed {
		if !inactivityLeak {
			n := baseReward * tgtWeight * (bal.PrevEpochTargetAttested / increment)
			reward += n / (activeIncrement * weightDenominator)
		}
	} else {
		penalty += baseReward * tgtWeight / weightDenominator
	}

	// Process head reward / penalty
	if v.IsPrevEpochHeadAttester && !v.IsSlashed {
		if !inactivityLeak {
			n := baseReward * headWeight * (bal.PrevEpochHeadAttested / increment)
			reward += n / (activeIncrement * weightDenominator)
		}
	}

	// Process finality delay penalty
	// Apply an additional penalty to validators that did not vote on the correct target or slashed
	if !v.IsPrevEpochTargetAttester || v.IsSlashed {
		n := effectiveBalance * v.InactivityScore
		penalty += n / inactivityDenominator
	}

	return reward, penalty
}
