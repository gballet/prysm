package testutil

import (
	"bytes"
	"encoding/binary"
	"testing"

	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/prysm/beacon-chain/core"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestBlockSignature(t *testing.T) {
	beaconState, privKeys := DeterministicGenesisState(t, 100)
	block, err := GenerateFullBlock(beaconState, privKeys, nil, 0)
	require.NoError(t, err)

	require.NoError(t, beaconState.SetSlot(beaconState.Slot()+1))
	proposerIdx, err := helpers.BeaconProposerIndex(beaconState)
	assert.NoError(t, err)

	assert.NoError(t, beaconState.SetSlot(beaconState.Slot()-1))
	epoch := core.SlotToEpoch(block.Block.Slot)
	blockSig, err := helpers.ComputeDomainAndSign(beaconState, epoch, block.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(t, err)

	signature, err := BlockSignature(beaconState, block.Block, privKeys)
	assert.NoError(t, err)

	if !bytes.Equal(blockSig, signature.Marshal()) {
		t.Errorf("Expected block signatures to be equal, received %#x != %#x", blockSig, signature.Marshal())
	}
}

func TestRandaoReveal(t *testing.T) {
	beaconState, privKeys := DeterministicGenesisState(t, 100)

	epoch := core.CurrentEpoch(beaconState)
	randaoReveal, err := RandaoReveal(beaconState, epoch, privKeys)
	assert.NoError(t, err)

	proposerIdx, err := helpers.BeaconProposerIndex(beaconState)
	assert.NoError(t, err)
	buf := make([]byte, 32)
	binary.LittleEndian.PutUint64(buf, uint64(epoch))
	// We make the previous validator's index sign the message instead of the proposer.
	sszUint := types.SSZUint64(epoch)
	epochSignature, err := helpers.ComputeDomainAndSign(beaconState, epoch, &sszUint, params.BeaconConfig().DomainRandao, privKeys[proposerIdx])
	require.NoError(t, err)

	if !bytes.Equal(randaoReveal, epochSignature) {
		t.Errorf("Expected randao reveals to be equal, received %#x != %#x", randaoReveal, epochSignature)
	}
}
