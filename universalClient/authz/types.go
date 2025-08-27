package authz

// KeyType represents the type of key being used for signing
type KeyType int

const (
	// UniversalValidatorHotKey represents the hot key used by Universal Validator
	UniversalValidatorHotKey KeyType = iota
)

// String returns string representation of KeyType
func (k KeyType) String() string {
	switch k {
	case UniversalValidatorHotKey:
		return "UniversalValidatorHotKey"
	default:
		return "Unknown"
	}
}

// DefaultAllowedMsgTypes defines the default message types that can be executed via AuthZ by the hot key
// These are the 4 standard Cosmos SDK message types that match the grants created in setup-container-authz
var DefaultAllowedMsgTypes = []string{
	// Bank module messages
	"/cosmos.bank.v1beta1.MsgSend",
	
	// Staking module messages
	"/cosmos.staking.v1beta1.MsgDelegate",
	"/cosmos.staking.v1beta1.MsgUndelegate",
	
	// Governance module messages
	"/cosmos.gov.v1beta1.MsgVote",
}

// UniversalValidatorMsgTypes defines the Universal Validator specific message types
// These will be used when the Universal Validator modules are available in the chain
var UniversalValidatorMsgTypes = []string{
	// Observer module messages
	"/push.observer.MsgVoteOnObservedEvent",
	"/push.observer.MsgSubmitObservation",
	
	// Registry module messages  
	"/push.uregistry.MsgUpdateRegistry",
	
	// Add other message types as needed for Universal Validator operations
	// "/push.validator.MsgUpdateValidatorInfo",
}

// AllowedMsgTypes holds the currently configured allowed message types
// This can be set to DefaultAllowedMsgTypes, UniversalValidatorMsgTypes, or custom types
var AllowedMsgTypes = DefaultAllowedMsgTypes

// IsAllowedMsgType checks if a message type is allowed for AuthZ execution
func IsAllowedMsgType(msgType string) bool {
	for _, allowed := range AllowedMsgTypes {
		if allowed == msgType {
			return true
		}
	}
	return false
}

// GetAllAllowedMsgTypes returns all allowed message types
func GetAllAllowedMsgTypes() []string {
	result := make([]string, len(AllowedMsgTypes))
	copy(result, AllowedMsgTypes)
	return result
}

// SetAllowedMsgTypes sets the allowed message types to a custom list
func SetAllowedMsgTypes(msgTypes []string) {
	AllowedMsgTypes = make([]string, len(msgTypes))
	copy(AllowedMsgTypes, msgTypes)
}

// UseDefaultMsgTypes sets the allowed message types to the default Cosmos SDK types
func UseDefaultMsgTypes() {
	AllowedMsgTypes = DefaultAllowedMsgTypes
}

// UseUniversalValidatorMsgTypes sets the allowed message types to Universal Validator specific types
func UseUniversalValidatorMsgTypes() {
	AllowedMsgTypes = UniversalValidatorMsgTypes
}

// GetMsgTypeCategory returns the category of message types currently in use
func GetMsgTypeCategory() string {
	switch {
	case len(AllowedMsgTypes) == len(DefaultAllowedMsgTypes) && 
		 AllowedMsgTypes[0] == DefaultAllowedMsgTypes[0]:
		return "default"
	case len(AllowedMsgTypes) == len(UniversalValidatorMsgTypes) && 
		 AllowedMsgTypes[0] == UniversalValidatorMsgTypes[0]:
		return "universal-validator"
	default:
		return "custom"
	}
}