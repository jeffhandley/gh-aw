//go:build !integration

package workflow

import (
	"slices"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// findNeedsJobRefs Tests
// =============================================================================

func TestFindNeedsJobRefs(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "empty content",
			content:  "",
			expected: nil,
		},
		{
			name:     "no needs references",
			content:  "Hello world ${{ github.event.issue.number }}",
			expected: nil,
		},
		{
			name:     "single custom job reference",
			content:  "${{ needs.my_job.outputs.result }}",
			expected: []string{"my_job"},
		},
		{
			name:     "multiple unique custom jobs",
			content:  "${{ needs.job_a.outputs.foo }} ${{ needs.job_b.outputs.bar }}",
			expected: []string{"job_a", "job_b"},
		},
		{
			name:     "duplicate references are deduplicated",
			content:  "${{ needs.my_job.outputs.x }} ${{ needs.my_job.outputs.y }}",
			expected: []string{"my_job"},
		},
		{
			name:     "pre_activation reference",
			content:  "${{ needs.pre_activation.outputs.activated }}",
			expected: []string{"pre_activation"},
		},
		{
			name:     "activation reference",
			content:  "${{ needs.activation.outputs.model }}",
			expected: []string{"activation"},
		},
		{
			name:     "hyphenated job name",
			content:  "${{ needs.my-job.outputs.result }}",
			expected: []string{"my-job"},
		},
		{
			name:     "result reference (not outputs)",
			content:  "${{ needs.my_job.result }}",
			expected: []string{"my_job"},
		},
		{
			name:     "sorted output",
			content:  "${{ needs.zebra.outputs.x }} ${{ needs.alpha.outputs.y }}",
			expected: []string{"alpha", "zebra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findNeedsJobRefs(tt.content)
			if len(tt.expected) == 0 {
				assert.Empty(t, got, "Expected no job references")
			} else {
				assert.Equal(t, tt.expected, got, "Job references should match")
			}
		})
	}
}

// =============================================================================
// buildEngineEnvContent Tests
// =============================================================================

func TestBuildEngineEnvContent(t *testing.T) {
	tests := []struct {
		name           string
		data           *WorkflowData
		expectNonEmpty bool
		expectContains string
	}{
		{
			name:           "nil EngineConfig",
			data:           &WorkflowData{},
			expectNonEmpty: false,
		},
		{
			name:           "empty Env map",
			data:           &WorkflowData{EngineConfig: &EngineConfig{Env: map[string]string{}}},
			expectNonEmpty: false,
		},
		{
			name: "single env entry",
			data: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"MY_VAR": "${{ needs.my_job.outputs.value }}",
					},
				},
			},
			expectNonEmpty: true,
			expectContains: "needs.my_job.outputs.value",
		},
		{
			name: "multiple env entries",
			data: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"VAR_A": "${{ needs.job_a.outputs.x }}",
						"VAR_B": "${{ needs.job_b.outputs.y }}",
					},
				},
			},
			expectNonEmpty: true,
			expectContains: "needs.job_a.outputs.x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEngineEnvContent(tt.data)
			if !tt.expectNonEmpty {
				assert.Empty(t, got, "Expected empty engine env content")
			} else {
				assert.NotEmpty(t, got, "Expected non-empty engine env content")
				if tt.expectContains != "" {
					assert.Contains(t, got, tt.expectContains, "Content should contain expected string")
				}
			}
		})
	}
}

// =============================================================================
// warnAboutExcludedBuiltinJobRefs Tests
// =============================================================================

func TestWarnAboutExcludedBuiltinJobRefs_NoWarnings(t *testing.T) {
	compiler := NewCompiler()
	initialWarnings := compiler.GetWarningCount()

	// Content with activation reference - activation IS in agentNeeds, so no warning
	compiler.warnAboutExcludedBuiltinJobRefs(
		[]string{"activation"},
		"${{ needs.activation.outputs.model }}",
	)
	assert.Equal(t, initialWarnings, compiler.GetWarningCount(), "Should not warn about activation when it is in needs list")
}

func TestWarnAboutExcludedBuiltinJobRefs_PreActivation(t *testing.T) {
	compiler := NewCompiler()
	initialWarnings := compiler.GetWarningCount()

	compiler.warnAboutExcludedBuiltinJobRefs(
		[]string{"activation"},
		"${{ needs.pre_activation.outputs.activated }}",
	)
	assert.Equal(t, initialWarnings+1, compiler.GetWarningCount(), "Should warn about pre_activation reference")
}

func TestWarnAboutExcludedBuiltinJobRefs_MultipleExcludedJobs(t *testing.T) {
	compiler := NewCompiler()
	initialWarnings := compiler.GetWarningCount()

	compiler.warnAboutExcludedBuiltinJobRefs(
		[]string{"activation"},
		"${{ needs.pre_activation.outputs.x }} ${{ needs.detection.outputs.y }}",
	)
	assert.Equal(t, initialWarnings+2, compiler.GetWarningCount(), "Should warn once per excluded built-in job")
}

func TestWarnAboutExcludedBuiltinJobRefs_CustomJobsNoWarning(t *testing.T) {
	compiler := NewCompiler()
	initialWarnings := compiler.GetWarningCount()

	// Custom (non-built-in) job references should never produce a warning
	compiler.warnAboutExcludedBuiltinJobRefs(
		[]string{"activation"},
		"${{ needs.my_custom_fetcher.outputs.value }}",
	)
	assert.Equal(t, initialWarnings, compiler.GetWarningCount(), "Should not warn about custom job references")
}

func TestWarnAboutExcludedBuiltinJobRefs_EmptyContent(t *testing.T) {
	compiler := NewCompiler()
	initialWarnings := compiler.GetWarningCount()

	compiler.warnAboutExcludedBuiltinJobRefs([]string{"activation"}, "")
	assert.Equal(t, initialWarnings, compiler.GetWarningCount(), "Should not warn on empty content")
}

func TestWarnAboutExcludedBuiltinJobRefs_MultipleContentParts(t *testing.T) {
	compiler := NewCompiler()
	initialWarnings := compiler.GetWarningCount()

	// pre_activation referenced across two content parts — should still produce exactly one warning
	compiler.warnAboutExcludedBuiltinJobRefs(
		[]string{"activation"},
		"${{ needs.pre_activation.outputs.activated }}", // content part 1
		"also ${{ needs.pre_activation.outputs.foo }}",  // content part 2 (same job)
	)
	assert.Equal(t, initialWarnings+1, compiler.GetWarningCount(), "Same built-in job across multiple content parts should warn only once")
}

// =============================================================================
// buildMainJob engine.env needs scanning Tests
// =============================================================================

func TestBuildMainJobEngineEnvCustomJobAddsToNeeds(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		Jobs: map[string]any{
			"my_fetcher": map[string]any{
				"runs-on": "ubuntu-latest",
				"outputs": map[string]any{
					"value": "${{ steps.fetch.outputs.value }}",
				},
				"steps": []any{
					map[string]any{"id": "fetch", "run": "echo value=test >> $GITHUB_OUTPUT"},
				},
			},
		},
		EngineConfig: &EngineConfig{
			Env: map[string]string{
				"MY_VALUE": "${{ needs.my_fetcher.outputs.value }}",
			},
		},
	}

	job, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed")

	assert.Truef(t, slices.Contains(job.Needs, "my_fetcher"),
		"my_fetcher should be in agent needs because it's referenced in engine.env; got: %v", job.Needs)
}

func TestBuildMainJobEngineEnvPreActivationExcludedAndWarned(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()
	initialWarnings := compiler.GetWarningCount()

	workflowData := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		EngineConfig: &EngineConfig{
			Env: map[string]string{
				// User mistakenly tries to pull pre_activation output into engine.env
				"MY_PARAM": "${{ needs.pre_activation.outputs.matched_command }}",
			},
		},
	}

	job, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed even when pre_activation is referenced in engine.env")

	assert.False(t, slices.Contains(job.Needs, string(constants.PreActivationJobName)),
		"pre_activation must NOT be in agent needs even when referenced in engine.env")

	assert.Equal(t, initialWarnings+1, compiler.GetWarningCount(),
		"A warning should be emitted for the pre_activation reference in engine.env")
}

func TestBuildMainJobMarkdownPreActivationRefWarned(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()
	initialWarnings := compiler.GetWarningCount()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		AI:              "copilot",
		RunsOn:          "runs-on: ubuntu-latest",
		MarkdownContent: "Use value ${{ needs.pre_activation.outputs.matched_command }} here.",
	}

	job, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed even when pre_activation is referenced in markdown")

	assert.False(t, slices.Contains(job.Needs, string(constants.PreActivationJobName)),
		"pre_activation must NOT be in agent needs when referenced in markdown")

	assert.Equal(t, initialWarnings+1, compiler.GetWarningCount(),
		"A warning should be emitted for the pre_activation reference in markdown")
}

func TestBuildMainJobEngineEnvNilConfigNoWarnings(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()
	initialWarnings := compiler.GetWarningCount()

	workflowData := &WorkflowData{
		Name:         "Test Workflow",
		AI:           "copilot",
		RunsOn:       "runs-on: ubuntu-latest",
		EngineConfig: nil,
	}

	_, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed when EngineConfig is nil")

	assert.Equal(t, initialWarnings, compiler.GetWarningCount(),
		"No warnings should be emitted when EngineConfig is nil")
}

func TestBuildMainJobEngineEnvActivationRefNoWarning(t *testing.T) {
	// activation IS already in the agent needs list — referencing its outputs in engine.env should not warn
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()
	initialWarnings := compiler.GetWarningCount()

	workflowData := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		EngineConfig: &EngineConfig{
			Env: map[string]string{
				"MODEL": "${{ needs.activation.outputs.model }}",
			},
		},
	}

	_, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed")

	assert.Equal(t, initialWarnings, compiler.GetWarningCount(),
		"No warning should be emitted for activation outputs — activation is in the agent needs list")
}

func TestBuildMainJobEngineEnvCustomJobNotDuplicatedInNeeds(t *testing.T) {
	// A custom job referenced both in the markdown body and in engine.env should appear only once in needs
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		AI:              "copilot",
		RunsOn:          "runs-on: ubuntu-latest",
		MarkdownContent: "Result: ${{ needs.my_fetcher.outputs.value }}",
		Jobs: map[string]any{
			"my_fetcher": map[string]any{
				"runs-on": "ubuntu-latest",
			},
		},
		EngineConfig: &EngineConfig{
			Env: map[string]string{
				"MY_VALUE": "${{ needs.my_fetcher.outputs.value }}",
			},
		},
	}

	job, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed")

	count := 0
	for _, need := range job.Needs {
		if need == "my_fetcher" {
			count++
		}
	}
	assert.Equalf(t, 1, count,
		"my_fetcher should appear exactly once in agent needs even when referenced in both markdown and engine.env; got needs: %v", job.Needs)
}
