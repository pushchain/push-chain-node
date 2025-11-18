package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	session "go-wrapper/go-dkls/sessions"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

// Service orchestrates DKLS keygen/refresh/sign flows over the transport.
type Service struct {
	cfg    Config
	deps   Dependencies
	logger zerolog.Logger

	mu       sync.RWMutex
	sessions map[string]*sessionState
}

// NewService wires up a Service instance.
func NewService(cfg Config, deps Dependencies) (*Service, error) {
	if cfg.PartyID == "" {
		return nil, errInvalidConfig
	}
	if deps.Transport == nil || deps.KeyshareStore == nil {
		return nil, errInvalidConfig
	}
	cfg.setDefaults()

	logger := cfg.Logger.With().
		Str("component", "tss_core").
		Str("party", cfg.PartyID).
		Logger()

	svc := &Service{
		cfg:      cfg,
		deps:     deps,
		logger:   logger,
		sessions: make(map[string]*sessionState),
	}

	if err := deps.Transport.RegisterHandler(svc.handleTransportMessage); err != nil {
		return nil, err
	}
	return svc, nil
}

// RunKeygen executes a DKLS key generation flow.
func (s *Service) RunKeygen(ctx context.Context, req KeygenRequest) (*KeygenResult, error) {
	if err := s.validateRequest(req.EventID, req.KeyID, req.Threshold, req.BlockNumber, req.Participants); err != nil {
		return nil, err
	}
	s.logger.Info().
		Str("event", req.EventID).
		Uint64("block", req.BlockNumber).
		Int("threshold", req.Threshold).
		Msg("starting keygen")
	exists, err := s.deps.KeyshareStore.Exists(req.KeyID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errKeyExists
	}

	// Determine if this node is coordinator (simplified - coordinator package handles this)
	isCoordinator := s.isCoordinatorForEvent(req.BlockNumber, req.Participants)

	result, err := s.runKeyshareProtocol(ctx, tss.ProtocolKeygen, req.EventID, req.KeyID, req.Threshold, req.BlockNumber, req.Participants, isCoordinator, func(parties *partySet) ([]byte, error) {
		return session.DklsKeygenSetupMsgNew(req.Threshold, deriveKeyID(req.KeyID), parties.encodedIDs())
	}, func(data []byte) (session.Handle, error) {
		return session.DklsKeygenSessionFromSetup(data, []byte(s.cfg.PartyID))
	})
	if err != nil {
		s.logger.Error().Err(err).Str("event", req.EventID).Msg("keygen failed")
		return nil, err
	}
	s.logger.Info().
		Str("event", req.EventID).
		Uint64("block", req.BlockNumber).
		Int("participants", result.NumParties).
		Msg("keygen finished")
	return &KeygenResult{KeyID: req.KeyID, PublicKey: result.PublicKey, NumParties: result.NumParties}, nil
}

// RunKeyrefresh executes DKLS key refresh.
func (s *Service) RunKeyrefresh(ctx context.Context, req KeyrefreshRequest) (*KeyrefreshResult, error) {
	if err := s.validateRequest(req.EventID, req.KeyID, req.Threshold, req.BlockNumber, req.Participants); err != nil {
		return nil, err
	}
	s.logger.Info().
		Str("event", req.EventID).
		Uint64("block", req.BlockNumber).
		Int("threshold", req.Threshold).
		Msg("starting keyrefresh")

	handle, cleanup, err := s.loadKeyshare(req.KeyID)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Determine if this node is coordinator
	isCoordinator := s.isCoordinatorForEvent(req.BlockNumber, req.Participants)

	result, err := s.runKeyshareProtocol(ctx, tss.ProtocolKeyrefresh, req.EventID, req.KeyID, req.Threshold, req.BlockNumber, req.Participants, isCoordinator, func(parties *partySet) ([]byte, error) {
		return session.DklsKeygenSetupMsgNew(req.Threshold, deriveKeyID(req.KeyID), parties.encodedIDs())
	}, func(data []byte) (session.Handle, error) {
		return session.DklsKeyRefreshSessionFromSetup(data, []byte(s.cfg.PartyID), handle)
	})
	if err != nil {
		s.logger.Error().Err(err).Str("event", req.EventID).Msg("keyrefresh failed")
		return nil, err
	}
	s.logger.Info().
		Str("event", req.EventID).
		Uint64("block", req.BlockNumber).
		Int("participants", result.NumParties).
		Msg("keyrefresh finished")
	return &KeyrefreshResult{KeyID: req.KeyID, PublicKey: result.PublicKey, NumParties: result.NumParties}, nil
}

// RunSign executes DKLS signing.
func (s *Service) RunSign(ctx context.Context, req SignRequest) (*SignResult, error) {
	if err := s.validateRequest(req.EventID, req.KeyID, req.Threshold, req.BlockNumber, req.Participants); err != nil {
		return nil, err
	}
	if len(req.MessageHash) == 0 {
		return nil, fmt.Errorf("message hash required")
	}
	s.logger.Info().
		Str("event", req.EventID).
		Uint64("block", req.BlockNumber).
		Int("threshold", req.Threshold).
		Msg("starting sign")

	handle, cleanup, err := s.loadKeyshare(req.KeyID)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	state, parties, err := s.prepareSession(tss.ProtocolSign, req.EventID, req.BlockNumber, req.Participants)
	if err != nil {
		return nil, err
	}
	defer s.unregisterSession(tss.ProtocolSign, req.EventID)

	if err := s.ensurePeers(parties); err != nil {
		return nil, err
	}

	isCoordinator := s.isCoordinatorForEvent(req.BlockNumber, req.Participants)
	if isCoordinator {
		s.logger.Info().
			Str("event", req.EventID).
			Uint64("block", req.BlockNumber).
			Msg("acting as coordinator for sign")

		// Give other nodes time to register their sessions
		s.logger.Info().
			Str("event", req.EventID).
			Msg("coordinator waiting for other nodes to register sessions")
		time.Sleep(5 * time.Second)

		// Use nil for empty chain path to avoid C wrapper panic
		chainPath := req.ChainPath
		if len(chainPath) == 0 {
			chainPath = nil
		}
		setup, err := session.DklsSignSetupMsgNew(deriveKeyID(req.KeyID), chainPath, req.MessageHash, parties.encodedIDs())
		if err != nil {
			return nil, err
		}
		env := s.buildSetupEnvelope(req.KeyID, req.Threshold, parties, setup)
		if err := state.enqueueSetup(env); err != nil {
			return nil, err
		}
		if err := s.broadcastSetup(ctx, tss.ProtocolSign, req.EventID, env, parties); err != nil {
			return nil, err
		}
	}

	setupEnv, err := s.waitForSetup(ctx, state)
	if err != nil {
		return nil, err
	}

	sessionHandle, err := session.DklsSignSessionFromSetup(setupEnv.Data, []byte(s.cfg.PartyID), handle)
	if err != nil {
		return nil, err
	}
	defer session.DklsSignSessionFree(sessionHandle)

	if err := s.sendSignOutputs(ctx, tss.ProtocolSign, req.EventID, sessionHandle, parties, state); err != nil {
		s.logger.Error().Err(err).Str("event", req.EventID).Msg("failed to send sign outputs")
		return nil, err
	}

	for {
		payload, err := s.waitForPayload(ctx, state)
		if err != nil {
			return nil, err
		}

		finished, err := session.DklsSignSessionInputMessage(sessionHandle, payload)
		if err != nil {
			return nil, err
		}

		if err := s.sendSignOutputs(ctx, tss.ProtocolSign, req.EventID, sessionHandle, parties, state); err != nil {
			s.logger.Error().Err(err).Str("event", req.EventID).Msg("failed to send sign outputs")
			return nil, err
		}

		if finished {
			sig, err := session.DklsSignSessionFinish(sessionHandle)
			if err != nil {
				return nil, err
			}

			// Log signature
			s.logger.Info().
				Str("event", req.EventID).
				Str("key_id", req.KeyID).
				Uint64("block", req.BlockNumber).
				Str("signature_hex", hex.EncodeToString(sig)).
				Int("participants", parties.len()).
				Msg("sign finished with signature")

			return &SignResult{KeyID: req.KeyID, Signature: sig, NumParties: parties.len()}, nil
		}
	}
}

func (s *Service) runKeyshareProtocol(
	ctx context.Context,
	protocol tss.ProtocolType,
	eventID, keyID string,
	threshold int,
	blockNumber uint64,
	participants []*tss.UniversalValidator,
	isCoordinator bool,
	setupBuilder func(*partySet) ([]byte, error),
	sessionBuilder func([]byte) (session.Handle, error),
) (*KeygenResult, error) {
	state, parties, err := s.prepareSession(protocol, eventID, blockNumber, participants)
	if err != nil {
		return nil, err
	}
	defer s.unregisterSession(protocol, eventID)

	if err := s.ensurePeers(parties); err != nil {
		return nil, err
	}

	if isCoordinator {
		s.logger.Info().
			Str("event", eventID).
			Uint64("block", blockNumber).
			Msg("acting as coordinator")

		// Give other nodes time to register their sessions
		// This is important in distributed systems where nodes process events asynchronously
		// We need enough time for all nodes to:
		// 1. Poll the database and see the event
		// 2. Pre-register their sessions
		// 3. Be ready to receive setup messages
		s.logger.Info().
			Str("event", eventID).
			Int("participants", parties.len()).
			Msg("coordinator waiting for other nodes to register sessions")
		time.Sleep(5 * time.Second)

		setup, err := setupBuilder(parties)
		if err != nil {
			return nil, err
		}
		env := s.buildSetupEnvelope(keyID, threshold, parties, setup)
		if err := state.enqueueSetup(env); err != nil {
			return nil, err
		}
		if err := s.broadcastSetup(ctx, protocol, eventID, env, parties); err != nil {
			return nil, err
		}
	}

	setupEnv, err := s.waitForSetup(ctx, state)
	if err != nil {
		return nil, err
	}

	sessionHandle, err := sessionBuilder(setupEnv.Data)
	if err != nil {
		return nil, err
	}
	defer session.DklsKeygenSessionFree(sessionHandle)

	if err := s.sendKeyshareOutputs(ctx, protocol, eventID, sessionHandle, parties, state); err != nil {
		return nil, err
	}

	for {
		payload, err := s.waitForPayload(ctx, state)
		if err != nil {
			return nil, err
		}

		finished, err := session.DklsKeygenSessionInputMessage(sessionHandle, payload)
		if err != nil {
			return nil, err
		}

		if err := s.sendKeyshareOutputs(ctx, protocol, eventID, sessionHandle, parties, state); err != nil {
			return nil, err
		}

		if finished {
			keyHandle, err := session.DklsKeygenSessionFinish(sessionHandle)
			if err != nil {
				return nil, err
			}
			defer session.DklsKeyshareFree(keyHandle)

			raw, err := session.DklsKeyshareToBytes(keyHandle)
			if err != nil {
				return nil, err
			}
			if err := s.deps.KeyshareStore.Store(raw, keyID); err != nil {
				return nil, err
			}
			pub, err := session.DklsKeysharePublicKey(keyHandle)
			if err != nil {
				return nil, err
			}

			// Log keyshare and pubkey
			protocolName := "keygen"
			if protocol == tss.ProtocolKeyrefresh {
				protocolName = "keyrefresh"
			}
			s.logger.Info().
				Str("key_id", keyID).
				Str("protocol", protocolName).
				// Str("keyshare_hex", hex.EncodeToString(raw)).
				Str("pubkey_hex", hex.EncodeToString(pub)).
				Int("participants", parties.len()).
				Msgf("%s completed with keyshare and pubkey", protocolName)

			return &KeygenResult{KeyID: keyID, PublicKey: pub, NumParties: parties.len()}, nil
		}
	}
}

func (s *Service) validateRequest(eventID, keyID string, threshold int, blockNumber uint64, participants []*tss.UniversalValidator) error {
	if eventID == "" || keyID == "" {
		return errInvalidConfig
	}
	if threshold <= 0 {
		return errMissingThreshold
	}
	if blockNumber == 0 {
		return fmt.Errorf("block number required")
	}
	if len(participants) == 0 {
		return errMissingParticipants
	}
	if threshold > len(participants) {
		return errMissingThreshold
	}
	return nil
}

func (s *Service) prepareSession(protocol tss.ProtocolType, eventID string, blockNumber uint64, participants []*tss.UniversalValidator) (*sessionState, *partySet, error) {
	parties, err := newPartySet(participants)
	if err != nil {
		return nil, nil, err
	}
	if !parties.contains(s.cfg.PartyID) {
		return nil, nil, errLocalNotIncluded
	}
	state, err := s.registerSession(protocol, eventID, blockNumber, parties)
	if err != nil {
		return nil, nil, err
	}
	return state, parties, nil
}

// RegisterSessionForEvent allows external callers (like coordinator) to pre-register a session
// before the coordinator broadcasts setup messages. This ensures all nodes have sessions ready.
func (s *Service) RegisterSessionForEvent(protocol tss.ProtocolType, eventID string, blockNumber uint64, participants []*tss.UniversalValidator) error {
	parties, err := newPartySet(participants)
	if err != nil {
		return err
	}
	if !parties.contains(s.cfg.PartyID) {
		return errLocalNotIncluded
	}
	_, err = s.registerSession(protocol, eventID, blockNumber, parties)
	return err
}

func deriveKeyID(keyID string) []byte {
	sum := sha256.Sum256([]byte(keyID))
	return sum[:]
}

func (s *Service) ensurePeers(parties *partySet) error {
	for _, p := range parties.list {
		if p.PartyID() == s.cfg.PartyID {
			continue
		}
		// Retry peer registration with exponential backoff
		maxRetries := 3
		var peerErr error
		for attempt := 0; attempt < maxRetries; attempt++ {
			if err := s.deps.Transport.EnsurePeer(p.PeerID(), p.Multiaddrs()); err != nil {
				peerErr = err
				if attempt < maxRetries-1 {
					backoff := time.Duration(attempt+1) * 500 * time.Millisecond
					s.logger.Debug().
						Str("peer_id", p.PeerID()).
						Int("attempt", attempt+1).
						Dur("backoff", backoff).
						Msg("retrying peer registration")
					time.Sleep(backoff)
					continue
				}
				return fmt.Errorf("failed to register peer %s after %d attempts: %w", p.PeerID(), maxRetries, peerErr)
			}
			peerErr = nil
			break
		}
		if peerErr != nil {
			return peerErr
		}
	}
	return nil
}

func (s *Service) buildSetupEnvelope(keyID string, threshold int, parties *partySet, data []byte) *setupEnvelope {
	env := &setupEnvelope{
		KeyID:        keyID,
		Threshold:    threshold,
		Data:         data,
		Participants: make([]participantEntry, 0, parties.len()),
	}
	for _, p := range parties.list {
		env.Participants = append(env.Participants, participantEntry{
			PartyID: p.PartyID(),
			PeerID:  p.PeerID(),
		})
	}
	return env
}

func (s *Service) broadcastSetup(ctx context.Context, protocol tss.ProtocolType, eventID string, env *setupEnvelope, parties *partySet) error {
	msg := &wireMessage{
		Protocol: protocol,
		Type:     messageSetup,
		EventID:  eventID,
		Setup:    env,
	}
	payload, err := encodeWire(msg)
	if err != nil {
		return err
	}

	// Send to all participants with retry logic
	var sendErrors []error
	for _, p := range parties.list {
		if p.PartyID() == s.cfg.PartyID {
			continue
		}
		s.logger.Debug().
			Str("event", eventID).
			Str("receiver", p.PartyID()).
			Msg("sending setup")

		// Retry sending with exponential backoff
		maxRetries := 3
		var sendErr error
		for attempt := 0; attempt < maxRetries; attempt++ {
			if err := s.deps.Transport.Send(ctx, p.PeerID(), payload); err != nil {
				sendErr = err
				if attempt < maxRetries-1 {
					backoff := time.Duration(attempt+1) * 500 * time.Millisecond
					s.logger.Debug().
						Str("event", eventID).
						Str("receiver", p.PartyID()).
						Int("attempt", attempt+1).
						Dur("backoff", backoff).
						Msg("retrying setup send")
					time.Sleep(backoff)
					continue
				}
				sendErrors = append(sendErrors, fmt.Errorf("failed to send setup to %s: %w", p.PartyID(), sendErr))
				s.logger.Error().
					Err(sendErr).
					Str("event", eventID).
					Str("receiver", p.PartyID()).
					Msg("failed to send setup after retries")
			} else {
				sendErr = nil
				break
			}
		}
		if sendErr != nil {
			// Error already added to sendErrors, continue to next peer
			continue
		}
	}

	// If we failed to send to any participant, return error
	if len(sendErrors) > 0 {
		return fmt.Errorf("failed to send setup to %d participants: %v", len(sendErrors), sendErrors)
	}

	return nil
}

func (s *Service) waitForSetup(ctx context.Context, state *sessionState) (*setupEnvelope, error) {
	timer := time.NewTimer(state.setupDeadline)
	defer timer.Stop()
	select {
	case env := <-state.setupCh:
		return env, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		s.logger.Error().
			Str("event", state.eventID).
			Msg("setup timeout")
		return nil, errSetupTimeout
	}
}

func (s *Service) waitForPayload(ctx context.Context, state *sessionState) ([]byte, error) {
	timer := time.NewTimer(state.payloadDeadline)
	defer timer.Stop()
	select {
	case payload := <-state.payloadCh:
		return payload, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		s.logger.Error().
			Str("event", state.eventID).
			Msg("payload timeout")
		return nil, errPayloadTimeout
	}
}

func (s *Service) sendKeyshareOutputs(
	ctx context.Context,
	protocol tss.ProtocolType,
	eventID string,
	handle session.Handle,
	parties *partySet,
	state *sessionState,
) error {
	for {
		msg, err := session.DklsKeygenSessionOutputMessage(handle)
		if err != nil {
			return err
		}
		if len(msg) == 0 {
			return nil
		}
		for idx := 0; idx < parties.len(); idx++ {
			receiver, err := session.DklsKeygenSessionMessageReceiver(handle, msg, idx)
			if err != nil {
				return err
			}
			if receiver == "" {
				break
			}
			if receiver == s.cfg.PartyID {
				if err := state.enqueuePayload(msg); err != nil {
					return err
				}
				continue
			}
			peer, ok := parties.peerInfo(receiver)
			if !ok {
				return fmt.Errorf("unknown receiver %s", receiver)
			}
			wire := &wireMessage{
				Protocol: protocol,
				Type:     messagePayload,
				EventID:  eventID,
				Payload:  msg,
			}
			payload, err := encodeWire(wire)
			if err != nil {
				return err
			}
			s.logger.Debug().
				Str("event", eventID).
				Str("receiver", receiver).
				Msg("sending payload")
			// Retry sending payload with exponential backoff
			maxRetries := 3
			var sendErr error
			for attempt := 0; attempt < maxRetries; attempt++ {
				if err := s.deps.Transport.Send(ctx, peer.PeerID(), payload); err != nil {
					sendErr = err
					if attempt < maxRetries-1 {
						backoff := time.Duration(attempt+1) * 200 * time.Millisecond
						s.logger.Debug().
							Str("event", eventID).
							Str("receiver", receiver).
							Int("attempt", attempt+1).
							Dur("backoff", backoff).
							Msg("retrying payload send")
						time.Sleep(backoff)
						continue
					}
					s.logger.Error().Err(err).
						Str("event", eventID).
						Str("receiver", receiver).
						Msg("failed to send payload after retries")
					return fmt.Errorf("failed to send payload to %s after %d attempts: %w", receiver, maxRetries, err)
				}
				sendErr = nil
				break
			}
			if sendErr != nil {
				return sendErr
			}
		}
	}
}

func (s *Service) sendSignOutputs(
	ctx context.Context,
	protocol tss.ProtocolType,
	eventID string,
	handle session.Handle,
	parties *partySet,
	state *sessionState,
) error {
	for {
		msg, err := session.DklsSignSessionOutputMessage(handle)
		if err != nil {
			return err
		}
		if len(msg) == 0 {
			return nil
		}
		for idx := 0; idx < parties.len(); idx++ {
			receiverBytes, err := session.DklsSignSessionMessageReceiver(handle, msg, idx)
			if err != nil {
				return err
			}
			receiver := string(receiverBytes)
			if receiver == "" {
				break
			}
			if receiver == s.cfg.PartyID {
				if err := state.enqueuePayload(msg); err != nil {
					return err
				}
				continue
			}
			peer, ok := parties.peerInfo(receiver)
			if !ok {
				return fmt.Errorf("unknown receiver %s", receiver)
			}
			wire := &wireMessage{
				Protocol: protocol,
				Type:     messagePayload,
				EventID:  eventID,
				Payload:  msg,
			}
			payload, err := encodeWire(wire)
			if err != nil {
				return err
			}
			s.logger.Debug().
				Str("event", eventID).
				Str("receiver", receiver).
				Msg("sending sign payload")
			// Retry sending sign payload with exponential backoff
			maxRetries := 3
			var sendErr error
			for attempt := 0; attempt < maxRetries; attempt++ {
				if err := s.deps.Transport.Send(ctx, peer.PeerID(), payload); err != nil {
					sendErr = err
					if attempt < maxRetries-1 {
						backoff := time.Duration(attempt+1) * 200 * time.Millisecond
						s.logger.Debug().
							Str("event", eventID).
							Str("receiver", receiver).
							Int("attempt", attempt+1).
							Dur("backoff", backoff).
							Msg("retrying sign payload send")
						time.Sleep(backoff)
						continue
					}
					s.logger.Error().Err(err).
						Str("event", eventID).
						Str("receiver", receiver).
						Msg("failed to send sign payload after retries")
					return fmt.Errorf("failed to send sign payload to %s after %d attempts: %w", receiver, maxRetries, err)
				}
				sendErr = nil
				break
			}
			if sendErr != nil {
				return sendErr
			}
		}
	}
}

// isCoordinatorForEvent determines if this node is the coordinator for an event.
// This is a simplified check - in production, the coordinator package handles this.
func (s *Service) isCoordinatorForEvent(blockNumber uint64, participants []*tss.UniversalValidator) bool {
	coordinatorParty := s.selectCoordinator(blockNumber, participants)
	return coordinatorParty == s.cfg.PartyID
}

// selectCoordinator selects the coordinator for a block number (simplified version).
// In production, the coordinator package uses more sophisticated logic with range sizes.
func (s *Service) selectCoordinator(blockNumber uint64, parties []*tss.UniversalValidator) string {
	if len(parties) == 0 {
		return ""
	}
	// Sort for consistency
	sorted := make([]*tss.UniversalValidator, len(parties))
	copy(sorted, parties)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PartyID() < sorted[j].PartyID()
	})
	idx := int(blockNumber % uint64(len(sorted)))
	if idx >= len(sorted) {
		return ""
	}
	return sorted[idx].PartyID()
}

func (s *Service) registerSession(protocol tss.ProtocolType, eventID string, blockNumber uint64, parties *partySet) (*sessionState, error) {
	key := sessionKey(protocol, eventID)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if session already exists
	if existing, exists := s.sessions[key]; exists {
		// If it's the same event, return the existing session
		// This can happen in production when multiple nodes try to process the same event
		s.logger.Debug().
			Str("event", eventID).
			Str("protocol", string(protocol)).
			Msg("session already exists, reusing")
		return existing, nil
	}

	state := newSessionState(protocol, eventID, s.cfg.SetupTimeout, s.cfg.MessageTimeout)
	// Determine coordinator (simplified - coordinator package handles this in production)
	// Convert partySet.list to []*tss.UniversalValidator for selectCoordinator
	participants := make([]*tss.UniversalValidator, len(parties.list))
	copy(participants, parties.list)
	coordinatorParty := s.selectCoordinator(blockNumber, participants)
	var coordinatorPeer string
	if coordinatorParty != "" {
		if peer, ok := parties.peerInfo(coordinatorParty); ok {
			coordinatorPeer = peer.PeerID()
		}
	}
	state.setMetadata(coordinatorPeer, parties)
	s.sessions[key] = state

	s.logger.Info().
		Str("event", eventID).
		Str("protocol", string(protocol)).
		Uint64("block", blockNumber).
		Str("coordinator", coordinatorParty).
		Int("participants", parties.len()).
		Msg("registered new session")

	return state, nil
}

func (s *Service) unregisterSession(protocol tss.ProtocolType, eventID string) {
	key := sessionKey(protocol, eventID)
	s.mu.Lock()
	delete(s.sessions, key)
	s.mu.Unlock()
}

func (s *Service) getSession(protocol tss.ProtocolType, eventID string) *sessionState {
	key := sessionKey(protocol, eventID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[key]
}

func sessionKey(protocol tss.ProtocolType, eventID string) string {
	return string(protocol) + ":" + eventID
}

func (s *Service) handleTransportMessage(ctx context.Context, sender string, payload []byte) error {
	msg, err := decodeWire(payload)
	if err != nil {
		s.logger.Error().
			Err(err).
			Str("sender", sender).
			Msg("failed to decode wire message")
		return fmt.Errorf("failed to decode message: %w", err)
	}

	// Sender is determined from transport layer, no need to validate

	state := s.getSession(msg.Protocol, msg.EventID)
	if state == nil {
		// Try to recover session from database if event exists and is PENDING
		// IN_PROGRESS means session already exists, so we don't recover in that case
		if s.deps.EventStore != nil {
			eventInfo, err := s.deps.EventStore.GetEvent(msg.EventID)
			if err != nil {
				s.logger.Debug().
					Err(err).
					Str("event", msg.EventID).
					Msg("failed to get event from store for session recovery")
			} else if eventInfo != nil && eventInfo.Status == "PENDING" {
				s.logger.Info().
					Str("event", msg.EventID).
					Str("protocol", string(msg.Protocol)).
					Msg("session not found, attempting to recover from database (event is PENDING)")

				// Create session from event info
				parties, err := newPartySet(eventInfo.Participants)
				if err != nil {
					s.logger.Warn().
						Err(err).
						Str("event", msg.EventID).
						Msg("failed to create party set for session recovery")
				} else {
					state, err = s.registerSession(msg.Protocol, msg.EventID, eventInfo.BlockNumber, parties)
					if err == nil {
						s.logger.Info().
							Str("event", msg.EventID).
							Int("participants", len(eventInfo.Participants)).
							Msg("recovered session from database")
					} else {
						s.logger.Warn().
							Err(err).
							Str("event", msg.EventID).
							Msg("failed to register recovered session")
					}
				}
			} else if eventInfo != nil {
				s.logger.Debug().
					Str("event", msg.EventID).
					Str("status", eventInfo.Status).
					Msg("event exists but status is not PENDING (IN_PROGRESS means session already exists)")
			}
		} else {
			s.logger.Debug().
				Str("event", msg.EventID).
				Msg("EventStore not available for session recovery")
		}

		if state == nil {
			s.logger.Debug().
				Str("event", msg.EventID).
				Str("sender", sender).
				Str("protocol", string(msg.Protocol)).
				Msg("dropping message for unknown session (may be from previous attempt)")
			return nil
		}
	}

	if !state.isParticipant(sender) {
		s.logger.Warn().
			Str("event", msg.EventID).
			Str("sender", sender).
			Str("protocol", string(msg.Protocol)).
			Msg("dropping message from unrecognized peer")
		return fmt.Errorf("unknown peer %s for event %s", sender, msg.EventID)
	}

	switch msg.Type {
	case messageSetup:
		if msg.Setup == nil {
			s.logger.Error().
				Str("event", msg.EventID).
				Str("sender", sender).
				Msg("received setup message without envelope")
			return fmt.Errorf("missing setup envelope")
		}
		if !state.isCoordinator(sender) {
			s.logger.Warn().
				Str("event", msg.EventID).
				Str("sender", sender).
				Str("expected_coordinator_peer", state.coordinatorPeerID).
				Msg("setup ignored because sender is not coordinator")
			return fmt.Errorf("setup from non-coordinator for event %s", msg.EventID)
		}
		s.logger.Info().
			Str("event", msg.EventID).
			Str("sender", sender).
			Int("threshold", msg.Setup.Threshold).
			Int("participants", len(msg.Setup.Participants)).
			Msg("received setup message from coordinator")
		return state.enqueueSetup(msg.Setup)
	case messagePayload:
		s.logger.Debug().
			Str("event", msg.EventID).
			Str("sender", sender).
			Int("payload_size", len(msg.Payload)).
			Msg("received payload message")
		return state.enqueuePayload(msg.Payload)
	default:
		s.logger.Error().
			Str("event", msg.EventID).
			Str("type", string(msg.Type)).
			Msg("unexpected message type")
		return fmt.Errorf("unexpected message type %s", msg.Type)
	}
}

func (s *Service) loadKeyshare(keyID string) (session.Handle, func(), error) {
	data, err := s.deps.KeyshareStore.Get(keyID)
	if err != nil {
		return 0, nil, err
	}
	handle, err := session.DklsKeyshareFromBytes(data)
	if err != nil {
		return 0, nil, err
	}
	return handle, func() {
		session.DklsKeyshareFree(handle)
	}, nil
}
