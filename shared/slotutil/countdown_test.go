package slotutil

import (
	"context"
	"testing"
	"time"

	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/shared/timeutils"
	"github.com/sirupsen/logrus"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

func TestCountdownToGenesis(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)

	hook := logTest.NewGlobal()
	params.SetupTestConfigCleanup(t)
	config := params.BeaconConfig()
	config.GenesisCountdownInterval = time.Millisecond * 500
	params.OverrideBeaconConfig(config)

	t.Run("normal countdown", func(t *testing.T) {
		defer hook.Reset()
		firstStringResult := "1s until chain genesis"
		genesisReached := "Chain genesis time reached"
		CountdownToGenesis(
			context.Background(),
			timeutils.Now().Add(2*time.Second),
			params.BeaconConfig().MinGenesisActiveValidatorCount,
			[32]byte{},
		)
		require.LogsContain(t, hook, firstStringResult)
		require.LogsContain(t, hook, genesisReached)
	})

	t.Run("close context", func(t *testing.T) {
		defer hook.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.AfterFunc(1500*time.Millisecond, func() {
				cancel()
			})
		}()
		CountdownToGenesis(
			ctx,
			timeutils.Now().Add(5*time.Second),
			params.BeaconConfig().MinGenesisActiveValidatorCount,
			[32]byte{},
		)
		require.LogsContain(t, hook, "4s until chain genesis")
		require.LogsContain(t, hook, "3s until chain genesis")
		require.LogsContain(t, hook, "Context closed, exiting routine")
		require.LogsDoNotContain(t, hook, "Chain genesis time reached")
	})
}
