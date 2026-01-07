package auth

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// PasswordPolicy defines password requirements
type PasswordPolicy struct {
	MinLength  int     // Minimum length (default: 12)
	MinEntropy float64 // Minimum bits of entropy (default: 60)
}

// DefaultPasswordPolicy returns the default password policy
// Focuses on entropy (n^m) where n = charset size, m = length
// 60 bits of entropy = ~1 quintillion combinations
func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength:  12,
		MinEntropy: 60.0, // ~1 quintillion combinations
	}
}

// PasswordStrength represents the calculated strength of a password
type PasswordStrength struct {
	Score       int      // 0-4 (very weak to strong)
	Length      int      // Password length
	Complexity  int      // Number of character classes used
	CharsetSize int      // Total number of possible characters (n)
	Entropy     float64  // Bits of entropy: log2(n^m) = m * log2(n)
	MeetsPolicy bool     // Does it meet the policy requirements
	Feedback    []string // Feedback messages for the user
}

// ValidatePassword validates a password against the policy
// username is optional - if provided, password cannot contain it
func ValidatePassword(password string, policy PasswordPolicy, username ...string) error {
	strength := CalculateStrength(password)

	// Check minimum length
	if len(password) < policy.MinLength {
		return fmt.Errorf("password must be at least %d characters", policy.MinLength)
	}

	// Check if password contains username
	if len(username) > 0 && username[0] != "" {
		// Case-insensitive check
		lowerPassword := strings.ToLower(password)
		lowerUsername := strings.ToLower(username[0])
		if strings.Contains(lowerPassword, lowerUsername) {
			return fmt.Errorf("password cannot contain your username")
		}
	}

	// Check for excessive repetition (including long repeated substrings)
	if hasExcessiveRepetition(password) {
		return fmt.Errorf("password has too much repetition")
	}

	// Check entropy (n^m combinations)
	if strength.Entropy < policy.MinEntropy {
		return fmt.Errorf("password is not strong enough (%.1f bits of entropy, need %.1f)",
			strength.Entropy, policy.MinEntropy)
	}

	return nil
}

// CalculateStrength calculates the strength of a password
// Uses entropy formula: entropy = length * log2(charset_size)
// where charset_size (n) depends on character classes used
func CalculateStrength(password string) PasswordStrength {
	strength := PasswordStrength{
		Length:   len(password),
		Feedback: make([]string, 0),
	}

	// Count character classes and determine charset size
	classes := getCharacterClasses(password)
	strength.Complexity = classes
	strength.CharsetSize = getCharsetSize(classes)

	// Calculate entropy: log2(n^m) = m * log2(n)
	// n = charset size, m = length
	strength.Entropy = float64(strength.Length) * math.Log2(float64(strength.CharsetSize))

	// Calculate score (0-4) based on entropy only
	strength.Score = calculateScore(strength.Entropy)

	// Generate feedback
	strength.Feedback = generateFeedback(password, strength.Entropy)

	// Note: MeetsPolicy is NOT calculated here to avoid recursion
	// Call ValidatePassword() separately to check if password meets policy
	strength.MeetsPolicy = false

	return strength
}

// getCharsetSize returns the total number of possible characters based on classes used
// This is 'n' in the n^m formula
func getCharsetSize(classes int) int {
	charsetSizes := map[int]int{
		1: 26, // Just lowercase (or just one class)
		2: 62, // lowercase + uppercase (26+26+10) or lowercase + digits
		3: 72, // lowercase + uppercase + digits (26+26+10+10 symbols subset)
		4: 95, // All printable ASCII (26+26+10+33 symbols)
	}

	if size, ok := charsetSizes[classes]; ok {
		return size
	}
	return 26 // Default to lowercase only
}

// getCharacterClasses counts how many character classes are used
// Classes: lowercase, uppercase, digits, symbols
func getCharacterClasses(password string) int {
	var hasLower, hasUpper, hasDigit, hasSymbol bool

	for _, char := range password {
		switch {
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSymbol = true
		}
	}

	count := 0
	if hasLower {
		count++
	}
	if hasUpper {
		count++
	}
	if hasDigit {
		count++
	}
	if hasSymbol {
		count++
	}

	return count
}

// calculateScore calculates a 0-4 score based on entropy
// Entropy thresholds:
// 0-40 bits: very weak (< ~1 trillion combinations)
// 40-50 bits: weak
// 50-60 bits: medium
// 60-70 bits: strong
// 70+ bits: very strong
func calculateScore(entropy float64) int {
	switch {
	case entropy >= 70:
		return 4 // Very strong
	case entropy >= 60:
		return 3 // Strong
	case entropy >= 50:
		return 2 // Medium
	case entropy >= 40:
		return 1 // Weak
	default:
		return 0 // Very weak
	}
}

// generateFeedback generates user-friendly feedback messages
func generateFeedback(password string, entropy float64) []string {
	feedback := make([]string, 0)

	length := len(password)

	// Entropy-based feedback
	if entropy < 40 {
		feedback = append(feedback, fmt.Sprintf("Very weak password (%.0f bits of entropy)", entropy))
		feedback = append(feedback, "Add more characters or use more character types")
	} else if entropy < 50 {
		feedback = append(feedback, fmt.Sprintf("Weak password (%.0f bits of entropy)", entropy))
		feedback = append(feedback, "Consider making it longer or more diverse")
	} else if entropy < 60 {
		feedback = append(feedback, fmt.Sprintf("Medium strength (%.0f bits of entropy)", entropy))
		feedback = append(feedback, "Good, but could be stronger")
	} else if entropy < 70 {
		feedback = append(feedback, fmt.Sprintf("Strong password (%.0f bits of entropy)", entropy))
	} else {
		feedback = append(feedback, fmt.Sprintf("Excellent password (%.0f bits of entropy)", entropy))
	}

	// Specific suggestions
	if length < 12 {
		feedback = append(feedback, fmt.Sprintf("Use at least 12 characters (currently %d)", length))
	}

	classes := getCharacterClasses(password)
	if classes == 1 && length < 20 {
		feedback = append(feedback, "Add uppercase, numbers, or symbols for better security")
	}

	return feedback
}

// hasExcessiveRepetition checks for repetitive patterns that weaken passwords
func hasExcessiveRepetition(password string) bool {
	if len(password) < 3 {
		return false
	}

	// Check for 3+ consecutive identical characters (e.g., "aaa", "111")
	// Since Go regexp doesn't support backreferences, check manually
	for i := 0; i < len(password)-2; i++ {
		if password[i] == password[i+1] && password[i] == password[i+2] {
			return true
		}
	}

	// Check for repeating substrings (e.g., "abcabc", "123123", "adminadmin")
	// Check from longer substrings first (2-20 characters)
	for length := min(20, len(password)/2); length >= 2; length-- {
		for i := 0; i <= len(password)-length*2; i++ {
			substring := password[i : i+length]
			nextPart := password[i+length : i+length*2]
			if substring == nextPart {
				return true
			}
		}
	}

	// Check for longer substrings appearing anywhere else (e.g., "adminXYZadmin")
	// Only check substrings of 4+ characters to avoid false positives
	for length := min(20, len(password)/2); length >= 4; length-- {
		for i := 0; i <= len(password)-length; i++ {
			substring := password[i : i+length]
			// Check if this substring appears anywhere after the current position
			restOfPassword := password[i+length:]
			if strings.Contains(restOfPassword, substring) {
				return true
			}
		}
	}

	// Check for sequential patterns (e.g., "abc", "123", "xyz")
	// Pattern: 4+ characters in ascending sequence
	sequentialCount := 0
	for i := 0; i < len(password)-1; i++ {
		if password[i+1] == password[i]+1 {
			sequentialCount++
			if sequentialCount >= 3 {
				return true
			}
		} else {
			sequentialCount = 0
		}
	}

	return false
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
