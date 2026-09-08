// Package console exposes the public façade of the Console BFF so
// sibling modules (notably tests/cross-module/chain-tenant-id) can
// drive the BFF HTTP handler stack without violating Go's
// `internal/` package rule.
//
// The façade is a thin wrapper over `console/internal/server`. It
// re-exports only the types and functions needed by external test
// fixtures. Internal handlers, hooks, and unexported helpers
// remain in `console/internal/server` and are not exposed.
//
// ADR-080 motivates this façade. The interface types
// (Backend) live in `console/pkg/bffbackend`; this package wires
// them together.
package console

import (
	"io.astrasync/console/internal/authflow"
	"io.astrasync/console/internal/server"
	"io.astrasync/console/pkg/bffbackend"
)

// Config is the public re-export of server.Config. See server.Config
// for field documentation.
type Config = server.Config

// Server is the public re-export of server.Server. The underlying
// type exposes the BFF HTTP handler via `Server.Handler()`.
type Server = server.Server

// Backend is a type alias for the public Backend interface.
type Backend = bffbackend.Backend

// SessionManager is the BFF's session manager interface. It is
// re-exported here so external fixtures can supply a SessionManager
// when calling NewWithConfig directly. Cross-module fixtures that do
// not need to drive the full session lifecycle should use
// NewWithDevelopmentSession instead.
type SessionManager = server.SessionManager

// NewWithConfig builds a Console BFF Server using the supplied
// configuration. Returns an error when Backend or Sessions is nil.
//
// The configuration's Sessions field must implement SessionManager.
// External callers that cannot import console/internal/authflow
// should use NewWithDevelopmentSession instead.
func NewWithConfig(config Config) (*Server, error) {
	return server.NewWithConfig(config)
}

// NewWithDevelopmentSession builds a Console BFF Server pre-wired
// with a development session manager whose sole active membership is
// keyed to the supplied tenantID and namespace. This helper exists
// for cross-module test fixtures (notably
// tests/cross-module/chain-tenant-id) that need to drive the BFF
// without depending on console/internal/authflow.
//
// The development manager is the same one console/internal/server.New
// builds, so behaviour matches what console/internal/server tests
// observe.
func NewWithDevelopmentSession(config Config, tenantID, namespace string) (*Server, error) {
	manager, err := authflow.NewDevelopmentManager(tenantID, namespace)
	if err != nil {
		return nil, err
	}
	config.Sessions = manager
	return server.NewWithConfig(config)
}
