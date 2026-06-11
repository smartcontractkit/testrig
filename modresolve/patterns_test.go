package modresolve_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/smartcontractkit/testrig/modresolve"
)

func TestGoTestFlagsBeforeArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no -args returns all",
			args: []string{"-v", "./pkg"},
			want: []string{"-v", "./pkg"},
		},
		{
			name: "stops before -args",
			args: []string{"-v", "./pkg", "-args", "-test.v"},
			want: []string{"-v", "./pkg"},
		},
		{
			name: "empty",
			args: nil,
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, modresolve.GoTestFlagsBeforeArgs(tc.args))
		})
	}
}

func TestPackagePatternsFromEnd(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"./core/...", "./foo"},
		modresolve.PackagePatternsFromEnd([]string{"-race", "-timeout=5m", "./core/...", "./foo"}),
	)
	assert.Nil(t, modresolve.PackagePatternsFromEnd([]string{"-v", "-race"}))
	assert.Equal(t,
		[]string{"./core/..."},
		modresolve.PackagePatternsFromEnd([]string{"-timeout", "10m", "./core/..."}),
	)
	assert.Nil(t, modresolve.PackagePatternsFromEnd([]string{"-timeout", "10m"}))
	assert.Equal(t,
		[]string{"./pkg"},
		modresolve.PackagePatternsFromEnd([]string{"./pkg", "-run", "TestName"}),
	)
	assert.Equal(t,
		[]string{"./pkg"},
		modresolve.PackagePatternsFromEnd([]string{"-run", "TestName", "./pkg"}),
	)
	assert.Equal(t,
		[]string{"./pkg"},
		modresolve.PackagePatternsFromEnd([]string{"./pkg", "-count=1"}),
	)
	assert.Equal(t,
		[]string{"./pkg"},
		modresolve.PackagePatternsFromEnd([]string{"./pkg", "-count", "1"}),
	)
}
