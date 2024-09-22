package reporulesetbot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRulesetFiles(t *testing.T) {
	t.Run("directory with multiple files", func(t *testing.T) {
		// Create a temporary directory
		dir, err := os.MkdirTemp("", "testdir")
		assert.NoError(t, err)
		defer os.RemoveAll(dir)

		// Create test files
		file1 := filepath.Join(dir, "file1.txt")
		file2 := filepath.Join(dir, "file2.txt")
		err = os.WriteFile(file1, []byte("content1"), 0644)
		assert.NoError(t, err)
		err = os.WriteFile(file2, []byte("content2"), 0644)
		assert.NoError(t, err)

		// Call the function
		files, err := getRulesetFiles(dir)
		assert.NoError(t, err)
		assert.ElementsMatch(t, []string{file1, file2}, files)
	})

	t.Run("directory with no files", func(t *testing.T) {
		// Create a temporary directory
		dir, err := os.MkdirTemp("", "emptydir")
		assert.NoError(t, err)
		defer os.RemoveAll(dir)

		// Call the function
		files, err := getRulesetFiles(dir)
		assert.NoError(t, err)
		assert.Empty(t, files)
	})

	t.Run("directory does not exist", func(t *testing.T) {
		// Call the function with a non-existent directory
		_, err := getRulesetFiles("nonexistentdir")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to read from directory nonexistentdir")
	})
}

func TestGetRepoFullNameFromURL(t *testing.T) {
	tests := []struct {
		name        string
		githubURL   string
		expected    string
		expectError bool
	}{
		{
			name:        "valid GitHub URL",
			githubURL:   "https://github.com/owner/repo",
			expected:    "owner/repo",
			expectError: false,
		},
		{
			name:        "valid GitHub URL with trailing slash",
			githubURL:   "https://github.com/owner/repo/",
			expected:    "owner/repo",
			expectError: false,
		},
		{
			name:        "invalid URL scheme",
			githubURL:   "ftp://github.com/owner/repo",
			expected:    "",
			expectError: true,
		},
		{
			name:        "invalid URL host",
			githubURL:   "https://example.com/owner/repo",
			expected:    "",
			expectError: true,
		},
		{
			name:        "URL without owner/repo",
			githubURL:   "https://github.com/",
			expected:    "",
			expectError: true,
		},
		{
			name:        "URL with extra segments",
			githubURL:   "https://github.com/owner/repo/extra",
			expected:    "",
			expectError: true,
		},
		{
			name:        "invalid URL",
			githubURL:   "://github.com/owner/repo",
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getRepoFullNameFromURL(tt.githubURL)
			if tt.expectError {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
