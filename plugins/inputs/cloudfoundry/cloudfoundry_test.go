package cloudfoundry

import (
	"testing"

	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

func TestCredentialsMissing(t *testing.T) {
	var err error
	var rec *Cloudfoundry

	rec = &Cloudfoundry{}
	err = rec.Start(&testutil.Accumulator{})
	require.EqualError(t, err, "missing creds")
	require.Error(t, err)
}
