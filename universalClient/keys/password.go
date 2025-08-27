package keys

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// PasswordManager handles secure password operations for hot keys
type PasswordManager struct {
	useFileCache bool
	cacheFile    string
}

// NewPasswordManager creates a new password manager
func NewPasswordManager(useFileCache bool, cacheFile string) *PasswordManager {
	return &PasswordManager{
		useFileCache: useFileCache,
		cacheFile:    cacheFile,
	}
}

// GetPassword securely prompts for password input
func (pm *PasswordManager) GetPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	
	// Check if we're running in an interactive terminal
	if !term.IsTerminal(int(syscall.Stdin)) {
		// Non-interactive mode - read from stdin
		reader := bufio.NewReader(os.Stdin)
		password, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read password from stdin: %w", err)
		}
		return strings.TrimSpace(password), nil
	}

	// Interactive mode - use secure terminal input
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	
	fmt.Println() // Add newline after password input
	
	return string(passwordBytes), nil
}

// GetPasswordWithConfirmation prompts for password and confirmation
func (pm *PasswordManager) GetPasswordWithConfirmation(prompt string) (string, error) {
	password, err := pm.GetPassword(prompt)
	if err != nil {
		return "", err
	}

	confirm, err := pm.GetPassword("Confirm password: ")
	if err != nil {
		return "", err
	}

	if password != confirm {
		return "", fmt.Errorf("passwords do not match")
	}

	return password, nil
}

// ValidatePasswordStrength validates password strength
func (pm *PasswordManager) ValidatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}

	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, char := range password {
		switch {
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= '0' && char <= '9':
			hasDigit = true
		case char >= 32 && char <= 126: // Printable ASCII special characters
			if !((char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')) {
				hasSpecial = true
			}
		}
	}

	var missing []string
	if !hasUpper {
		missing = append(missing, "uppercase letter")
	}
	if !hasLower {
		missing = append(missing, "lowercase letter")
	}
	if !hasDigit {
		missing = append(missing, "digit")
	}
	if !hasSpecial {
		missing = append(missing, "special character")
	}

	if len(missing) > 0 {
		return fmt.Errorf("password must contain at least one: %s", strings.Join(missing, ", "))
	}

	return nil
}

// PromptForSecurePassword prompts for a new secure password with validation
func (pm *PasswordManager) PromptForSecurePassword() (string, error) {
	for {
		password, err := pm.GetPasswordWithConfirmation("Enter new password: ")
		if err != nil {
			return "", err
		}

		if err := pm.ValidatePasswordStrength(password); err != nil {
			fmt.Printf("Password validation failed: %s\n", err)
			fmt.Println("Please try again with a stronger password.")
			continue
		}

		return password, nil
	}
}

// PromptForExistingPassword prompts for an existing password
func (pm *PasswordManager) PromptForExistingPassword() (string, error) {
	return pm.GetPassword("Enter password: ")
}

// SecurePasswordInput handles secure password input based on keyring backend
func SecurePasswordInput(backend KeyringBackend, operation string) (string, error) {
	pm := NewPasswordManager(false, "")

	switch backend {
	case KeyringBackendFile:
		switch operation {
		case "create":
			fmt.Println("Creating new encrypted key. Please provide a strong password.")
			return pm.PromptForSecurePassword()
		case "access":
			return pm.PromptForExistingPassword()
		default:
			return "", fmt.Errorf("unknown password operation: %s", operation)
		}
	case KeyringBackendTest:
		// Test backend doesn't require passwords
		return "", nil
	default:
		return "", fmt.Errorf("unsupported keyring backend: %s", backend)
	}
}

// IsSecureEnvironment checks if the current environment is secure for password operations
func IsSecureEnvironment() SecurityCheck {
	checks := SecurityCheck{
		IsInteractive:    term.IsTerminal(int(syscall.Stdin)),
		HasSecureInput:   true, // Assume secure input is available
		EnvironmentSafe:  true, // Basic assumption
		Recommendations: []string{},
	}

	if !checks.IsInteractive {
		checks.Recommendations = append(checks.Recommendations, "Consider running in interactive mode for secure password input")
	}

	// Check if we're in a secure shell session
	if os.Getenv("SSH_CONNECTION") != "" {
		checks.Recommendations = append(checks.Recommendations, "SSH session detected - ensure connection is secure")
	}

	// Check for screen recording or remote access indicators
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		checks.Recommendations = append(checks.Recommendations, "Terminal multiplexer detected - verify session security")
	}

	return checks
}

// SecurityCheck represents the results of a security environment check
type SecurityCheck struct {
	IsInteractive     bool     `json:"is_interactive"`
	HasSecureInput    bool     `json:"has_secure_input"`
	EnvironmentSafe   bool     `json:"environment_safe"`
	Recommendations   []string `json:"recommendations"`
}

// DisplaySecurityWarnings displays security warnings and recommendations
func (sc SecurityCheck) DisplaySecurityWarnings() {
	if !sc.IsInteractive {
		fmt.Println("⚠️  Warning: Non-interactive terminal detected")
	}

	if !sc.HasSecureInput {
		fmt.Println("⚠️  Warning: Secure input not available")
	}

	if len(sc.Recommendations) > 0 {
		fmt.Println("\n🔒 Security Recommendations:")
		for _, rec := range sc.Recommendations {
			fmt.Printf("   • %s\n", rec)
		}
		fmt.Println()
	}
}

// GetSecurePasswordForKeyring gets password for keyring operations with security checks
func GetSecurePasswordForKeyring(backend KeyringBackend, operation string, skipSecurityCheck bool) (string, error) {
	// Perform security check unless skipped
	if !skipSecurityCheck {
		secCheck := IsSecureEnvironment()
		secCheck.DisplaySecurityWarnings()
		
		if !secCheck.EnvironmentSafe && !secCheck.IsInteractive {
			return "", fmt.Errorf("environment is not secure for password input")
		}
	}

	// Get password based on backend and operation
	return SecurePasswordInput(backend, operation)
}