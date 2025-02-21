package fork

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang/snappy"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/transition"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	v1 "github.com/prysmaticlabs/prysm/beacon-chain/state/v1"
	stateAltair "github.com/prysmaticlabs/prysm/beacon-chain/state/v2"
	ethpb "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1/wrapper"
	wrapperv1 "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1/wrapper"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/spectest/utils"
)

type ForkConfig struct {
	PostFork    string `json:"post_fork"`
	ForkEpoch   int    `json:"fork_epoch"`
	ForkBlock   int    `json:"fork_block"`
	BlocksCount int    `json:"blocks_count"`
}

// RunForkTransitionTest is a helper function that runs Altair's transition core tests.
func RunForkTransitionTest(t *testing.T, config string) {
	require.NoError(t, utils.SetConfig(t, config))

	testFolders, testsFolderPath := utils.TestFolders(t, config, "altair", "transition/core/pyspec_tests")
	for _, folder := range testFolders {
		t.Run(folder.Name(), func(t *testing.T) {
			helpers.ClearCache()
			file, err := testutil.BazelFileBytes(testsFolderPath, folder.Name(), "meta.yaml")
			require.NoError(t, err)
			config := &ForkConfig{}
			require.NoError(t, utils.UnmarshalYaml(file, config), "Failed to Unmarshal")

			preforkBlocks := make([]*ethpb.SignedBeaconBlock, 0)
			postforkBlocks := make([]*ethpb.SignedBeaconBlockAltair, 0)
			// Fork happens without any pre-fork blocks.
			if config.ForkBlock == 0 {
				for i := 0; i < config.BlocksCount; i++ {
					fileName := fmt.Sprint("blocks_", i, ".ssz_snappy")
					blockFile, err := testutil.BazelFileBytes(testsFolderPath, folder.Name(), fileName)
					require.NoError(t, err)
					blockSSZ, err := snappy.Decode(nil /* dst */, blockFile)
					require.NoError(t, err, "Failed to decompress")
					block := &ethpb.SignedBeaconBlockAltair{}
					require.NoError(t, block.UnmarshalSSZ(blockSSZ), "Failed to unmarshal")
					postforkBlocks = append(postforkBlocks, block)
				}
				// Fork happens with pre-fork blocks.
			} else {
				for i := 0; i <= config.ForkBlock; i++ {
					fileName := fmt.Sprint("blocks_", i, ".ssz_snappy")
					blockFile, err := testutil.BazelFileBytes(testsFolderPath, folder.Name(), fileName)
					require.NoError(t, err)
					blockSSZ, err := snappy.Decode(nil /* dst */, blockFile)
					require.NoError(t, err, "Failed to decompress")
					block := &ethpb.SignedBeaconBlock{}
					require.NoError(t, block.UnmarshalSSZ(blockSSZ), "Failed to unmarshal")
					preforkBlocks = append(preforkBlocks, block)
				}
				for i := config.ForkBlock + 1; i < config.BlocksCount; i++ {
					fileName := fmt.Sprint("blocks_", i, ".ssz_snappy")
					blockFile, err := testutil.BazelFileBytes(testsFolderPath, folder.Name(), fileName)
					require.NoError(t, err)
					blockSSZ, err := snappy.Decode(nil /* dst */, blockFile)
					require.NoError(t, err, "Failed to decompress")
					block := &ethpb.SignedBeaconBlockAltair{}
					require.NoError(t, block.UnmarshalSSZ(blockSSZ), "Failed to unmarshal")
					postforkBlocks = append(postforkBlocks, block)
				}
			}

			preBeaconStateFile, err := testutil.BazelFileBytes(testsFolderPath, folder.Name(), "pre.ssz_snappy")
			require.NoError(t, err)
			preBeaconStateSSZ, err := snappy.Decode(nil /* dst */, preBeaconStateFile)
			require.NoError(t, err, "Failed to decompress")
			beaconStateBase := &ethpb.BeaconState{}
			require.NoError(t, beaconStateBase.UnmarshalSSZ(preBeaconStateSSZ), "Failed to unmarshal")
			beaconState, err := v1.InitializeFromProto(beaconStateBase)
			require.NoError(t, err)

			bc := params.BeaconConfig()
			bc.AltairForkEpoch = types.Epoch(config.ForkEpoch)
			params.OverrideBeaconConfig(bc)

			ctx := context.Background()
			var ok bool
			for _, b := range preforkBlocks {
				st, err := transition.ExecuteStateTransition(ctx, beaconState, wrapperv1.WrappedPhase0SignedBeaconBlock(b))
				require.NoError(t, err)
				beaconState, ok = st.(*v1.BeaconState)
				require.Equal(t, true, ok)
			}
			altairState := state.BeaconStateAltair(beaconState)
			for _, b := range postforkBlocks {
				wsb, err := wrapper.WrappedAltairSignedBeaconBlock(b)
				require.NoError(t, err)
				st, err := transition.ExecuteStateTransition(ctx, altairState, wsb)
				require.NoError(t, err)
				altairState, ok = st.(*stateAltair.BeaconState)
				require.Equal(t, true, ok)
			}

			postBeaconStateFile, err := testutil.BazelFileBytes(testsFolderPath, folder.Name(), "post.ssz_snappy")
			require.NoError(t, err)
			postBeaconStateSSZ, err := snappy.Decode(nil /* dst */, postBeaconStateFile)
			require.NoError(t, err, "Failed to decompress")
			postBeaconState := &ethpb.BeaconStateAltair{}
			require.NoError(t, postBeaconState.UnmarshalSSZ(postBeaconStateSSZ), "Failed to unmarshal")

			pbState, err := stateAltair.ProtobufBeaconState(altairState.CloneInnerState())
			require.NoError(t, err)
			require.DeepSSZEqual(t, pbState, postBeaconState)
		})
	}
}
