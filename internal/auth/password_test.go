package auth

import (
	"testing"
)

// TestValidatePassword tests the main password validation function
func TestValidatePassword(t *testing.T) {
	policy := DefaultPasswordPolicy()

	tests := []struct {
		name      string
		password  string
		username  string
		wantError bool
		errorMsg  string
	}{
		// Happy paths
		{
			name:      "strong password with good entropy",
			password:  "MyS3cur3P@ssw0rd!",
			username:  "",
			wantError: false,
		},
		{
			name:      "long lowercase password",
			password:  "verylonglowercasepassword", // No repetition
			username:  "",
			wantError: false,
		},
		{
			name:      "password with diverse characters",
			password:  "Abc123!@#XyzPqr",
			username:  "testuser",
			wantError: false,
		},

		// Sad paths - too short
		{
			name:      "password too short",
			password:  "Short1!",
			username:  "",
			wantError: true,
			errorMsg:  "password must be at least 12 characters",
		},

		// Sad paths - contains username
		{
			name:      "password contains username",
			password:  "MyAdminPassword123",
			username:  "admin",
			wantError: true,
			errorMsg:  "password cannot contain your username",
		},
		{
			name:      "password contains username case insensitive",
			password:  "password_TESTUSER_123",
			username:  "testuser",
			wantError: true,
			errorMsg:  "password cannot contain your username",
		},

		// Sad paths - weak entropy
		{
			name:      "weak all lowercase",
			password:  "passwordonly", // 12 chars, ~56 bits
			username:  "",
			wantError: true,
			errorMsg:  "password is not strong enough",
		},

		// Sad paths - excessive repetition
		{
			name:      "repeated characters",
			password:  "Mypassword111",
			username:  "",
			wantError: true,
			errorMsg:  "password has too much repetition",
		},
		{
			name:      "repeated substring adjacent",
			password:  "abcabc123456", // 12 chars
			username:  "",
			wantError: true,
			errorMsg:  "password has too much repetition",
		},
		{
			name:      "repeated long substring",
			password:  "adminadmin12", // 12 chars
			username:  "",
			wantError: true,
			errorMsg:  "password has too much repetition",
		},
		{
			name:      "sequential pattern",
			password:  "abcdefgh1234",
			username:  "",
			wantError: true,
			errorMsg:  "password has too much repetition",
		},

		// Sad paths - insufficient entropy
		{
			name:      "low entropy all lowercase",
			password:  "lowentropynow", // 13 chars, ~61 bits - just over threshold
			username:  "",
			wantError: false, // This one passes now with 61 bits
			errorMsg:  "password is not strong enough",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.username != "" {
				err = ValidatePassword(tt.password, policy, tt.username)
			} else {
				err = ValidatePassword(tt.password, policy)
			}

			if tt.wantError {
				if err == nil {
					t.Errorf("ValidatePassword() expected error but got nil")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("ValidatePassword() error = %v, want error containing %v", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidatePassword() unexpected error = %v", err)
				}
			}
		})
	}
}

// TestCalculateStrength tests entropy calculation and scoring
func TestCalculateStrength(t *testing.T) {
	tests := []struct {
		name           string
		password       string
		wantMinEntropy float64
		wantMaxEntropy float64
		wantMinScore   int
		wantMaxScore   int
		wantClasses    int
	}{
		{
			name:           "all lowercase 12 chars",
			password:       "abcdefghijkl",
			wantMinEntropy: 56.0,
			wantMaxEntropy: 57.0,
			wantMinScore:   2,
			wantMaxScore:   2,
			wantClasses:    1,
		},
		{
			name:           "all lowercase 13 chars",
			password:       "abcdefghijklm",
			wantMinEntropy: 61.0,
			wantMaxEntropy: 62.0,
			wantMinScore:   3,
			wantMaxScore:   3,
			wantClasses:    1,
		},
		{
			name:           "mixed case 12 chars",
			password:       "AbCdEfGhIjKl",
			wantMinEntropy: 71.0,
			wantMaxEntropy: 72.0,
			wantMinScore:   4,
			wantMaxScore:   4,
			wantClasses:    2,
		},
		{
			name:           "all character types",
			password:       "Abc123!@#Xyz",
			wantMinEntropy: 68.0,
			wantMaxEntropy: 80.0,
			wantMinScore:   3,
			wantMaxScore:   4,
			wantClasses:    4,
		},
		{
			name:           "very long password",
			password:       "thisisaverylongpassword", // 23 chars
			wantMinEntropy: 108.0,
			wantMaxEntropy: 109.0,
			wantMinScore:   4,
			wantMaxScore:   4,
			wantClasses:    1,
		},
		{
			name:           "short weak password",
			password:       "weak",
			wantMinEntropy: 18.0,
			wantMaxEntropy: 20.0,
			wantMinScore:   0,
			wantMaxScore:   0,
			wantClasses:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strength := CalculateStrength(tt.password)

			if strength.Entropy < tt.wantMinEntropy || strength.Entropy > tt.wantMaxEntropy {
				t.Errorf("CalculateStrength() entropy = %v, want between %v and %v",
					strength.Entropy, tt.wantMinEntropy, tt.wantMaxEntropy)
			}

			if strength.Score < tt.wantMinScore || strength.Score > tt.wantMaxScore {
				t.Errorf("CalculateStrength() score = %v, want between %v and %v",
					strength.Score, tt.wantMinScore, tt.wantMaxScore)
			}

			if strength.Complexity != tt.wantClasses {
				t.Errorf("CalculateStrength() complexity = %v, want %v",
					strength.Complexity, tt.wantClasses)
			}

			if strength.Length != len(tt.password) {
				t.Errorf("CalculateStrength() length = %v, want %v",
					strength.Length, len(tt.password))
			}
		})
	}
}

// TestHasExcessiveRepetition tests repetition detection
func TestHasExcessiveRepetition(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     bool
	}{
		// Happy paths - no excessive repetition
		{
			name:     "normal password",
			password: "MySecureP@ss",
			want:     false,
		},
		{
			name:     "varied characters",
			password: "Abc123XyzPqr",
			want:     false,
		},
		{
			name:     "two repeated characters allowed",
			password: "password11",
			want:     false,
		},
		{
			name:     "short password no repetition",
			password: "ab",
			want:     false,
		},

		// Sad paths - character repetition
		{
			name:     "three repeated characters",
			password: "passsword",
			want:     true,
		},
		{
			name:     "repeated digits",
			password: "mypass111",
			want:     true,
		},
		{
			name:     "repeated special chars",
			password: "mypass!!!",
			want:     true,
		},

		// Sad paths - substring repetition (adjacent)
		{
			name:     "repeated 2-char substring",
			password: "ababpassword",
			want:     true,
		},
		{
			name:     "repeated 3-char substring",
			password: "abcabcpassword",
			want:     true,
		},
		{
			name:     "repeated 4-char substring",
			password: "testtest123",
			want:     true,
		},

		// Sad paths - substring repetition (non-adjacent)
		{
			name:     "admin repeated",
			password: "adminXYZadmin",
			want:     true,
		},
		{
			name:     "password repeated",
			password: "pass123pass456",
			want:     true,
		},
		{
			name:     "long substring repeated",
			password: "securepassXYZsecurepass",
			want:     true,
		},

		// Sad paths - sequential patterns
		{
			name:     "sequential lowercase",
			password: "myabcdpass",
			want:     true,
		},
		{
			name:     "sequential digits",
			password: "pass1234word",
			want:     true,
		},
		{
			name:     "sequential uppercase",
			password: "PASSABCDword",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasExcessiveRepetition(tt.password)
			if got != tt.want {
				t.Errorf("hasExcessiveRepetition(%q) = %v, want %v", tt.password, got, tt.want)
			}
		})
	}
}

// TestGetCharsetSize tests charset size calculation
func TestGetCharsetSize(t *testing.T) {
	tests := []struct {
		name    string
		classes int
		want    int
	}{
		{
			name:    "one character class",
			classes: 1,
			want:    26,
		},
		{
			name:    "two character classes",
			classes: 2,
			want:    62,
		},
		{
			name:    "three character classes",
			classes: 3,
			want:    72,
		},
		{
			name:    "four character classes",
			classes: 4,
			want:    95,
		},
		{
			name:    "zero classes defaults to lowercase",
			classes: 0,
			want:    26,
		},
		{
			name:    "invalid high number defaults to lowercase",
			classes: 10,
			want:    26,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getCharsetSize(tt.classes)
			if got != tt.want {
				t.Errorf("getCharsetSize(%v) = %v, want %v", tt.classes, got, tt.want)
			}
		})
	}
}

// TestGetCharacterClasses tests character class counting
func TestGetCharacterClasses(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     int
	}{
		{
			name:     "only lowercase",
			password: "abcdefgh",
			want:     1,
		},
		{
			name:     "only uppercase",
			password: "ABCDEFGH",
			want:     1,
		},
		{
			name:     "only digits",
			password: "12345678",
			want:     1,
		},
		{
			name:     "only symbols",
			password: "!@#$%^&*",
			want:     1,
		},
		{
			name:     "lowercase and uppercase",
			password: "AbCdEfGh",
			want:     2,
		},
		{
			name:     "lowercase and digits",
			password: "abc12345",
			want:     2,
		},
		{
			name:     "three classes",
			password: "Abc12345",
			want:     3,
		},
		{
			name:     "all four classes",
			password: "Abc123!@",
			want:     4,
		},
		{
			name:     "empty string",
			password: "",
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getCharacterClasses(tt.password)
			if got != tt.want {
				t.Errorf("getCharacterClasses(%q) = %v, want %v", tt.password, got, tt.want)
			}
		})
	}
}

// TestCalculateScore tests the scoring function
func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name    string
		entropy float64
		want    int
	}{
		{
			name:    "very weak entropy",
			entropy: 30.0,
			want:    0,
		},
		{
			name:    "weak entropy",
			entropy: 45.0,
			want:    1,
		},
		{
			name:    "medium entropy",
			entropy: 55.0,
			want:    2,
		},
		{
			name:    "strong entropy",
			entropy: 65.0,
			want:    3,
		},
		{
			name:    "very strong entropy",
			entropy: 75.0,
			want:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateScore(tt.entropy)
			if got != tt.want {
				t.Errorf("calculateScore(%v) = %v, want %v",
					tt.entropy, got, tt.want)
			}
		})
	}
}

// TestGenerateFeedback tests feedback generation
func TestGenerateFeedback(t *testing.T) {
	tests := []struct {
		name           string
		password       string
		entropy        float64
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:         "very weak password",
			password:     "weak",
			entropy:      35.0,
			wantContains: []string{"Very weak", "35 bits"},
		},
		{
			name:         "weak password",
			password:     "weakpass",
			entropy:      45.0,
			wantContains: []string{"Weak password", "45 bits"},
		},
		{
			name:         "medium password",
			password:     "mediumpass12",
			entropy:      55.0,
			wantContains: []string{"Medium strength", "55 bits"},
		},
		{
			name:         "strong password",
			password:     "StrongPass123",
			entropy:      65.0,
			wantContains: []string{"Strong password", "65 bits"},
		},
		{
			name:         "excellent password",
			password:     "ExcellentP@ssw0rd!",
			entropy:      75.0,
			wantContains: []string{"Excellent", "75 bits"},
		},
		{
			name:         "short password",
			password:     "short",
			entropy:      40.0,
			wantContains: []string{"at least 12 characters"},
		},
		{
			name:         "single class password",
			password:     "onlylowercase",
			entropy:      60.0,
			wantContains: []string{"Add uppercase, numbers, or symbols"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feedback := generateFeedback(tt.password, tt.entropy)
			feedbackStr := joinFeedback(feedback)

			for _, want := range tt.wantContains {
				if !contains(feedbackStr, want) {
					t.Errorf("generateFeedback() feedback = %v, want to contain %v",
						feedbackStr, want)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if contains(feedbackStr, notWant) {
					t.Errorf("generateFeedback() feedback = %v, should not contain %v",
						feedbackStr, notWant)
				}
			}
		})
	}
}

// Helper functions for tests

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func joinFeedback(feedback []string) string {
	result := ""
	for i, f := range feedback {
		if i > 0 {
			result += " "
		}
		result += f
	}
	return result
}
