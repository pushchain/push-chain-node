package evm

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"

	"github.com/rollchains/pchain/universalClient/chains/common"
	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// EventParser handles parsing of EVM gateway events
type EventParser struct {
	gatewayAddr ethcommon.Address
	config      *uregistrytypes.ChainConfig
	eventTopics map[string]ethcommon.Hash
	logger      zerolog.Logger
}

// NewEventParser creates a new event parser
func NewEventParser(
	gatewayAddr ethcommon.Address,
	config *uregistrytypes.ChainConfig,
	logger zerolog.Logger,
) *EventParser {
	// Build event topics from config methods
	eventTopics := make(map[string]ethcommon.Hash)
	logger.Info().
		Int("gateway_methods_count", len(config.GatewayMethods)).
		Str("gateway_address", config.GatewayAddress).
		Msg("building event topics")
	
	for _, method := range config.GatewayMethods {
		if method.EventIdentifier != "" {
			eventTopics[method.Identifier] = ethcommon.HexToHash(method.EventIdentifier)
			logger.Info().
				Str("method", method.Name).
				Str("event_identifier", method.EventIdentifier).
				Str("method_id", method.Identifier).
				Msg("registered event topic from config")
		} else {
			logger.Warn().
				Str("method", method.Name).
				Str("method_id", method.Identifier).
				Msg("no event identifier provided in config for method")
		}
	}

	return &EventParser{
		gatewayAddr: gatewayAddr,
		config:      config,
		eventTopics: eventTopics,
		logger:      logger.With().Str("component", "evm_event_parser").Logger(),
	}
}

// ParseGatewayEvent parses a log into a GatewayEvent
func (ep *EventParser) ParseGatewayEvent(log *types.Log) *common.GatewayEvent {
	if len(log.Topics) == 0 {
		return nil
	}

	// Find matching method by event topic
	var methodID, methodName, confirmationType string
	for id, topic := range ep.eventTopics {
		if log.Topics[0] == topic {
			methodID = id
			// Find method name and confirmation type from config
			for _, method := range ep.config.GatewayMethods {
				if method.Identifier == id {
					methodName = method.Name
					// Map confirmation type enum to string
					if method.ConfirmationType == 2 { // CONFIRMATION_TYPE_FAST
						confirmationType = "FAST"
					} else {
						confirmationType = "STANDARD" // Default to STANDARD
					}
					break
				}
			}
			break
		}
	}

	if methodID == "" {
		return nil
	}

	event := &common.GatewayEvent{
		ChainID:          ep.config.Chain,
		TxHash:           log.TxHash.Hex(),
		BlockNumber:      log.BlockNumber,
		Method:           methodName,
		EventID:          methodID,
		Payload:          log.Data,
		ConfirmationType: confirmationType,
	}

	// Parse event data based on method
	ep.parseEventData(event, log, methodName)

	return event
}

// parseEventData extracts specific data from the event based on method type
func (ep *EventParser) parseEventData(event *common.GatewayEvent, log *types.Log, methodName string) {
	if methodName == "addFunds" && len(log.Topics) >= 3 {
		// FundsAdded event typically has:
		// topics[0] = event signature
		// topics[1] = indexed sender address
		// topics[2] = indexed token address (or other indexed param)
		// data contains amount and payload
		
		event.Sender = ethcommon.BytesToAddress(log.Topics[1].Bytes()).Hex()
		
		// Parse amount from data if available
		if len(log.Data) >= 32 {
			amount := new(big.Int).SetBytes(log.Data[:32])
			event.Amount = amount.String()
		}
		
		// If there's a receiver in topics (depends on event structure)
		if len(log.Topics) >= 3 {
			// This might be token address or receiver, depends on the specific event
			event.Receiver = ethcommon.BytesToAddress(log.Topics[2].Bytes()).Hex()
		}
	}
	// Add more event parsing logic for other methods as needed
}

// GetEventTopics returns the configured event topics
func (ep *EventParser) GetEventTopics() []ethcommon.Hash {
	topics := make([]ethcommon.Hash, 0, len(ep.eventTopics))
	for _, topic := range ep.eventTopics {
		topics = append(topics, topic)
	}
	return topics
}

// HasEvents checks if any event topics are configured
func (ep *EventParser) HasEvents() bool {
	return len(ep.eventTopics) > 0
}