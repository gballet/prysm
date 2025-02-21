package validator

import (
	"context"
	"testing"
	"time"

	types "github.com/prysmaticlabs/eth2-types"
	mockChain "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/feed"
	opfeed "github.com/prysmaticlabs/prysm/beacon-chain/core/feed/operation"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/transition"
	"github.com/prysmaticlabs/prysm/beacon-chain/operations/voluntaryexits"
	mockp2p "github.com/prysmaticlabs/prysm/beacon-chain/p2p/testing"
	mockSync "github.com/prysmaticlabs/prysm/beacon-chain/sync/initial-sync/testing"
	ethpb "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestProposeExit_Notification(t *testing.T) {
	ctx := context.Background()

	deposits, keys, err := testutil.DeterministicDepositsAndKeys(params.BeaconConfig().MinGenesisActiveValidatorCount)
	require.NoError(t, err)
	beaconState, err := transition.GenesisBeaconState(ctx, deposits, 0, &ethpb.Eth1Data{BlockHash: make([]byte, 32)})
	require.NoError(t, err)
	epoch := types.Epoch(2048)
	require.NoError(t, beaconState.SetSlot(params.BeaconConfig().SlotsPerEpoch.Mul(uint64(epoch))))
	block := testutil.NewBeaconBlock()
	genesisRoot, err := block.Block.HashTreeRoot()
	require.NoError(t, err, "Could not get signing root")

	// Set genesis time to be 100 epochs ago.
	offset := int64(params.BeaconConfig().SlotsPerEpoch.Mul(params.BeaconConfig().SecondsPerSlot))
	genesisTime := time.Now().Add(time.Duration(-100*offset) * time.Second)
	mockChainService := &mockChain.ChainService{State: beaconState, Root: genesisRoot[:], Genesis: genesisTime}
	server := &Server{
		HeadFetcher:       mockChainService,
		SyncChecker:       &mockSync.Sync{IsSyncing: false},
		TimeFetcher:       mockChainService,
		StateNotifier:     mockChainService.StateNotifier(),
		OperationNotifier: mockChainService.OperationNotifier(),
		ExitPool:          voluntaryexits.NewPool(),
		P2P:               mockp2p.NewTestP2P(t),
	}

	// Subscribe to operation notifications.
	opChannel := make(chan *feed.Event, 1024)
	opSub := server.OperationNotifier.OperationFeed().Subscribe(opChannel)
	defer opSub.Unsubscribe()

	// Send the request, expect a result on the state feed.
	validatorIndex := types.ValidatorIndex(0)
	req := &ethpb.SignedVoluntaryExit{
		Exit: &ethpb.VoluntaryExit{
			Epoch:          epoch,
			ValidatorIndex: validatorIndex,
		},
	}
	req.Signature, err = helpers.ComputeDomainAndSign(beaconState, epoch, req.Exit, params.BeaconConfig().DomainVoluntaryExit, keys[0])
	require.NoError(t, err)

	resp, err := server.ProposeExit(context.Background(), req)
	require.NoError(t, err)
	expectedRoot, err := req.Exit.HashTreeRoot()
	require.NoError(t, err)
	assert.DeepEqual(t, expectedRoot[:], resp.ExitRoot)

	// Ensure the state notification was broadcast.
	notificationFound := false
	for !notificationFound {
		select {
		case event := <-opChannel:
			if event.Type == opfeed.ExitReceived {
				notificationFound = true
				data, ok := event.Data.(*opfeed.ExitReceivedData)
				assert.Equal(t, true, ok, "Entity is not of type *opfeed.ExitReceivedData")
				assert.Equal(t, epoch, data.Exit.Exit.Epoch, "Unexpected state feed epoch")
				assert.Equal(t, validatorIndex, data.Exit.Exit.ValidatorIndex, "Unexpected state feed validator index")
			}
		case <-opSub.Err():
			t.Error("Subscription to state notifier failed")
			return
		}
	}
}

func TestProposeExit_NoPanic(t *testing.T) {
	ctx := context.Background()

	deposits, keys, err := testutil.DeterministicDepositsAndKeys(params.BeaconConfig().MinGenesisActiveValidatorCount)
	require.NoError(t, err)
	beaconState, err := transition.GenesisBeaconState(ctx, deposits, 0, &ethpb.Eth1Data{BlockHash: make([]byte, 32)})
	require.NoError(t, err)
	epoch := types.Epoch(2048)
	require.NoError(t, beaconState.SetSlot(params.BeaconConfig().SlotsPerEpoch.Mul(uint64(epoch))))
	block := testutil.NewBeaconBlock()
	genesisRoot, err := block.Block.HashTreeRoot()
	require.NoError(t, err, "Could not get signing root")

	// Set genesis time to be 100 epochs ago.
	offset := int64(params.BeaconConfig().SlotsPerEpoch.Mul(params.BeaconConfig().SecondsPerSlot))
	genesisTime := time.Now().Add(time.Duration(-100*offset) * time.Second)
	mockChainService := &mockChain.ChainService{State: beaconState, Root: genesisRoot[:], Genesis: genesisTime}
	server := &Server{
		HeadFetcher:       mockChainService,
		SyncChecker:       &mockSync.Sync{IsSyncing: false},
		TimeFetcher:       mockChainService,
		StateNotifier:     mockChainService.StateNotifier(),
		OperationNotifier: mockChainService.OperationNotifier(),
		ExitPool:          voluntaryexits.NewPool(),
		P2P:               mockp2p.NewTestP2P(t),
	}

	// Subscribe to operation notifications.
	opChannel := make(chan *feed.Event, 1024)
	opSub := server.OperationNotifier.OperationFeed().Subscribe(opChannel)
	defer opSub.Unsubscribe()

	req := &ethpb.SignedVoluntaryExit{}
	_, err = server.ProposeExit(context.Background(), req)
	require.ErrorContains(t, "voluntary exit does not exist", err, "Expected error for no exit existing")

	// Send the request, expect a result on the state feed.
	validatorIndex := types.ValidatorIndex(0)
	req = &ethpb.SignedVoluntaryExit{
		Exit: &ethpb.VoluntaryExit{
			Epoch:          epoch,
			ValidatorIndex: validatorIndex,
		},
	}

	_, err = server.ProposeExit(context.Background(), req)
	require.ErrorContains(t, "invalid signature provided", err, "Expected error for no signature exists")
	req.Signature = bytesutil.FromBytes48([48]byte{})

	_, err = server.ProposeExit(context.Background(), req)
	require.ErrorContains(t, "invalid signature provided", err, "Expected error for invalid signature length")
	req.Signature, err = helpers.ComputeDomainAndSign(beaconState, epoch, req.Exit, params.BeaconConfig().DomainVoluntaryExit, keys[0])
	require.NoError(t, err)
	resp, err := server.ProposeExit(context.Background(), req)
	require.NoError(t, err)
	expectedRoot, err := req.Exit.HashTreeRoot()
	require.NoError(t, err)
	assert.DeepEqual(t, expectedRoot[:], resp.ExitRoot)
}
