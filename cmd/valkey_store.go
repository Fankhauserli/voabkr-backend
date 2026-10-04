package main

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/gob"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gorilla/securecookie"
	gsessions "github.com/gorilla/sessions"
	"github.com/valkey-io/valkey-go"
)

// ValkeyStore implements gin-contrib/sessions.Store using valkey.Client,
// providing full transparent support for multi-shard Valkey / Redis clusters as well as standalone instances.
type ValkeyStore struct {
	client    valkey.Client
	codecs    []securecookie.Codec
	options   *gsessions.Options
	keyPrefix string
	maxLength int
}

// NewValkeyStore creates a new ValkeyStore backed by a cluster-aware valkey.Client.
func NewValkeyStore(client valkey.Client, keyPairs ...[]byte) *ValkeyStore {
	return &ValkeyStore{
		client: client,
		codecs: securecookie.CodecsFromPairs(keyPairs...),
		options: &gsessions.Options{
			Path:   "/",
			MaxAge: 86400,
		},
		keyPrefix: "session_",
		maxLength: 4096,
	}
}

// Options sets the session cookie options.
func (s *ValkeyStore) Options(options sessions.Options) {
	s.options = options.ToGorillaOptions()
}

// Get returns a cached session from the request registry.
func (s *ValkeyStore) Get(r *http.Request, name string) (*gsessions.Session, error) {
	return gsessions.GetRegistry(r).Get(s, name)
}

// New returns a session for the given name without adding it to the registry.
func (s *ValkeyStore) New(r *http.Request, name string) (*gsessions.Session, error) {
	session := gsessions.NewSession(s, name)
	opts := *s.options
	session.Options = &opts
	session.IsNew = true

	var err error
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, s.codecs...)
		if err == nil {
			ok, loadErr := s.load(session)
			if loadErr == nil && ok {
				session.IsNew = false
			} else if loadErr != nil {
				err = loadErr
			}
		}
	}
	return session, err
}

// Save persists the session to Valkey and sets the response cookie.
func (s *ValkeyStore) Save(r *http.Request, w http.ResponseWriter, session *gsessions.Session) error {
	if session.Options.MaxAge <= 0 {
		if err := s.delete(session); err != nil {
			return err
		}
		http.SetCookie(w, gsessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	if session.ID == "" {
		session.ID = strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)), "=")
	}

	if err := s.save(session); err != nil {
		return err
	}

	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, s.codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, gsessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

func (s *ValkeyStore) save(session *gsessions.Session) error {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	if err := enc.Encode(session.Values); err != nil {
		return err
	}
	b := buf.Bytes()
	if s.maxLength > 0 && len(b) > s.maxLength {
		return errors.New("valkeystore: session data exceeds maxLength")
	}

	age := session.Options.MaxAge
	if age <= 0 {
		age = 86400
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := s.client.B().Set().
		Key(s.keyPrefix + session.ID).
		Value(string(b)).
		ExSeconds(int64(age)).
		Build()

	return s.client.Do(ctx, cmd).Error()
}

func (s *ValkeyStore) load(session *gsessions.Session) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := s.client.B().Get().Key(s.keyPrefix + session.ID).Build()
	res := s.client.Do(ctx, cmd)
	if valkey.IsValkeyNil(res.Error()) {
		return false, nil
	}
	if err := res.Error(); err != nil {
		return false, err
	}

	b, err := res.AsBytes()
	if err != nil {
		return false, err
	}

	dec := gob.NewDecoder(bytes.NewBuffer(b))
	if err := dec.Decode(&session.Values); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ValkeyStore) delete(session *gsessions.Session) error {
	if session.ID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := s.client.B().Del().Key(s.keyPrefix + session.ID).Build()
	return s.client.Do(ctx, cmd).Error()
}
