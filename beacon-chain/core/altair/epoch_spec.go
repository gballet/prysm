package altair

import (
	"context"

	"github.com/prysmaticlabs/prysm/beacon-chain/core"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// ProcessSyncCommitteeUpdates  processes sync client committee updates for the beacon state.
//
// Spec code:
// def process_sync_committee_updates(state: BeaconState) -> None:
//    next_epoch = get_current_epoch(state) + Epoch(1)
//    if next_epoch % EPOCHS_PER_SYNC_COMMITTEE_PERIOD == 0:
//        state.current_sync_committee = state.next_sync_committee
//        state.next_sync_committee = get_next_sync_committee(state)
func ProcessSyncCommitteeUpdates(ctx context.Context, beaconState state.BeaconStateAltair) (state.BeaconStateAltair, error) {
	nextEpoch := core.NextEpoch(beaconState)
	if nextEpoch%params.BeaconConfig().EpochsPerSyncCommitteePeriod == 0 {
		nextSyncCommittee, err := beaconState.NextSyncCommittee()
		if err != nil {
			return nil, err
		}
		if err := beaconState.SetCurrentSyncCommittee(nextSyncCommittee); err != nil {
			return nil, err
		}
		nextSyncCommittee, err = NextSyncCommittee(ctx, beaconState)
		if err != nil {
			return nil, err
		}
		if err := beaconState.SetNextSyncCommittee(nextSyncCommittee); err != nil {
			return nil, err
		}
		if err := helpers.UpdateSyncCommitteeCache(beaconState); err != nil {
			return nil, err
		}
	}
	return beaconState, nil
}

// ProcessParticipationFlagUpdates processes participation flag updates by rotating current to previous.
//
// Spec code:
// def process_participation_flag_updates(state: BeaconState) -> None:
//    state.previous_epoch_participation = state.current_epoch_participation
//    state.current_epoch_participation = [ParticipationFlags(0b0000_0000) for _ in range(len(state.validators))]
func ProcessParticipationFlagUpdates(beaconState state.BeaconStateAltair) (state.BeaconStateAltair, error) {
	c, err := beaconState.CurrentEpochParticipation()
	if err != nil {
		return nil, err
	}
	if err := beaconState.SetPreviousParticipationBits(c); err != nil {
		return nil, err
	}
	if err := beaconState.SetCurrentParticipationBits(make([]byte, beaconState.NumValidators())); err != nil {
		return nil, err
	}
	return beaconState, nil
}
