package management

import (
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/bedrockmantle"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type mantlePendingSession struct {
	mu       sync.Mutex
	storage  *bedrockmantle.Storage
	client   *bedrockmantle.Client
	accounts []bedrockmantle.Account
	roles    []string
	status   string
	created  time.Time
	original *coreauth.Auth
}

type mantleSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*mantlePendingSession
}

var mantleSessions = &mantleSessionStore{sessions: make(map[string]*mantlePendingSession)}

const mantleSessionTTL = 20 * time.Minute

func (s *mantleSessionStore) put(state string, sess *mantlePendingSession) {
	sess.created = time.Now()
	sess.status = "pending"
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if time.Since(v.created) > mantleSessionTTL {
			delete(s.sessions, k)
		}
	}
	s.sessions[state] = sess
}

func (s *mantleSessionStore) get(state string) *mantlePendingSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[state]
	if sess != nil && (time.Since(sess.created) > mantleSessionTTL || !IsOAuthSessionPending(state, mantleProvider)) {
		delete(s.sessions, state)
		return nil
	}
	return sess
}

func (s *mantleSessionStore) del(state string) {
	s.mu.Lock()
	delete(s.sessions, state)
	s.mu.Unlock()
}
