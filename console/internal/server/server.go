package server

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"io.astrasync/console/internal/authflow"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

const (
	defaultPageSize    = 50
	maximumPageSize    = 100
	readTimeout        = 5 * time.Second
	writeTimeout       = 15 * time.Second
	maximumBodySize    = 256 * 1024
	adminSessionCookie = "__Host-astra_session"
	loginCookie        = "__Host-astra_login"
)

//go:embed web/*
var staticFiles embed.FS

type SessionManager interface {
	BeginLogin(context.Context, string) (authflow.LoginStart, error)
	CompleteLogin(context.Context, string, string, string) (authflow.Session, string, error)
	Resolve(context.Context, string) (authflow.Session, error)
	ValidateCSRF(authflow.Session, string) bool
	Logout(context.Context, authflow.Session) error
}

type Config struct {
	Backend      any
	Sessions     SessionManager
	Namespace    string
	PublicOrigin string
	AuthMode     string
	Ready        func(context.Context) error
	MaximumBody  int64
}

type Server struct {
	jobs         JobReader
	catalog      CatalogReader
	connections  ConnectionClient
	mutations    JobMutationClient
	validator    JobValidator
	audit        AuditReader
	sessions     SessionManager
	namespace    string
	publicOrigin string
	authMode     string
	ready        func(context.Context) error
	legacy       bool
	maximumBody  int64
}

// New preserves the development-only read-only constructor used by the first
// Console slice. Production callers should use NewWithConfig.
func New(reader JobReader, namespace string) (*Server, error) {
	if reader == nil {
		return nil, fmt.Errorf("job reader must not be nil")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("console namespace must not be empty")
	}
	development, err := authflow.NewDevelopmentManager("00000000-0000-4000-8000-000000000001", namespace)
	if err != nil {
		return nil, err
	}
	return &Server{jobs: reader, sessions: development, namespace: namespace, authMode: "disabled", legacy: true,
		maximumBody: maximumBodySize}, nil
}

func NewWithConfig(configuration Config) (*Server, error) {
	if configuration.Backend == nil || configuration.Sessions == nil {
		return nil, fmt.Errorf("Console backend and sessions must not be nil")
	}
	authMode := strings.ToLower(strings.TrimSpace(configuration.AuthMode))
	if authMode == "" {
		authMode = "oidc"
	}
	if authMode != "oidc" && authMode != "disabled" {
		return nil, fmt.Errorf("Console auth mode must be oidc or disabled")
	}
	maximumBody := configuration.MaximumBody
	if maximumBody == 0 {
		maximumBody = maximumBodySize
	}
	if maximumBody < 1024 || maximumBody > 4*1024*1024 {
		return nil, fmt.Errorf("Console maximum body size is invalid")
	}
	server := &Server{sessions: configuration.Sessions, namespace: strings.TrimSpace(configuration.Namespace),
		publicOrigin: strings.TrimRight(strings.TrimSpace(configuration.PublicOrigin), "/"), authMode: authMode,
		ready: configuration.Ready, maximumBody: maximumBody}
	server.jobs, _ = configuration.Backend.(JobReader)
	server.catalog, _ = configuration.Backend.(CatalogReader)
	server.connections, _ = configuration.Backend.(ConnectionClient)
	server.mutations, _ = configuration.Backend.(JobMutationClient)
	server.validator, _ = configuration.Backend.(JobValidator)
	server.audit, _ = configuration.Backend.(AuditReader)
	if server.jobs == nil {
		return nil, fmt.Errorf("Console backend does not implement JobReader")
	}
	if authMode == "oidc" && server.publicOrigin == "" {
		return nil, fmt.Errorf("Console public origin is required in OIDC mode")
	}
	return server, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.readyHandler)
	mux.HandleFunc("GET /auth/login", s.login)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /api/session", s.currentSession)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("GET /api/jobs/{name}", s.getJob)
	mux.HandleFunc("GET /api/jobs/{name}/status", s.getJobStatus)
	if s.mutations != nil {
		mux.HandleFunc("POST /api/jobs", s.createJob)
		mux.HandleFunc("PUT /api/jobs/{name}", s.updateJob)
		mux.HandleFunc("DELETE /api/jobs/{name}", s.deleteJob)
		mux.HandleFunc("POST /api/jobs/{name}/start", s.startJob)
		mux.HandleFunc("POST /api/jobs/{name}/stop", s.stopJob)
		if s.validator != nil {
			mux.HandleFunc("POST /api/jobs/{name}/validate", s.validateJob)
		}
	}
	if s.catalog != nil {
		mux.HandleFunc("GET /api/connectors", s.listConnectors)
		mux.HandleFunc("GET /api/connectors/{name}", s.getConnector)
	}
	if s.connections != nil {
		mux.HandleFunc("GET /api/connections", s.listConnections)
		mux.HandleFunc("POST /api/connections", s.createConnection)
		mux.HandleFunc("GET /api/connections/{name}", s.getConnection)
		mux.HandleFunc("PUT /api/connections/{name}", s.updateConnection)
		mux.HandleFunc("POST /api/connections/{name}/rotate", s.rotateConnection)
		mux.HandleFunc("POST /api/connections/{name}/enable", s.enableConnection)
		mux.HandleFunc("POST /api/connections/{name}/disable", s.disableConnection)
		mux.HandleFunc("DELETE /api/connections/{name}", s.deleteConnection)
		mux.HandleFunc("POST /api/connections/{name}/test", s.testConnection)
		mux.HandleFunc("GET /api/connection-tests/{operationID}", s.getConnectionTest)
	}
	if s.audit != nil {
		mux.HandleFunc("GET /api/audit-events", s.listAuditEvents)
	}

	content, err := fs.Sub(staticFiles, "web")
	if err != nil {
		panic(fmt.Sprintf("embedded Console assets: %v", err))
	}
	mux.Handle("/", http.FileServer(http.FS(content)))
	return securityHeaders(mux)
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ok\n"))
}

func (s *Server) readyHandler(response http.ResponseWriter, request *http.Request) {
	if s.ready != nil {
		ctx, cancel := context.WithTimeout(request.Context(), time.Second)
		defer cancel()
		if err := s.ready(ctx); err != nil {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
	} else if s.jobs != nil {
		ctx, cancel := context.WithTimeout(request.Context(), time.Second)
		defer cancel()
		_, err := s.jobs.ListJobs(ctx, &jobv1.ListJobsRequest{Namespace: s.namespace, Page: 1, PageSize: 1})
		if err != nil {
			http.Error(response, "control plane unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ready\n"))
}

func (s *Server) login(response http.ResponseWriter, request *http.Request) {
	if s.authMode != "oidc" {
		writeError(response, status.Error(codes.NotFound, "login is unavailable"))
		return
	}
	start, err := s.sessions.BeginLogin(request.Context(), request.URL.Query().Get("return_to"))
	if err != nil {
		writeError(response, status.Error(codes.Unavailable, "authentication provider unavailable"))
		return
	}
	http.SetCookie(response, &http.Cookie{Name: loginCookie, Value: start.BrowserBinding, Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(time.Until(start.ExpiresAt).Seconds())})
	http.Redirect(response, request, start.AuthorizationURL, http.StatusFound)
}

func (s *Server) callback(response http.ResponseWriter, request *http.Request) {
	if s.authMode != "oidc" {
		writeError(response, status.Error(codes.NotFound, "login is unavailable"))
		return
	}
	cookie, err := request.Cookie(loginCookie)
	if err != nil || cookie.Value == "" {
		writeError(response, status.Error(codes.Unauthenticated, "login transaction is unavailable"))
		return
	}
	if request.URL.Query().Get("error") != "" {
		writeError(response, status.Error(codes.Unauthenticated, "authentication was canceled"))
		return
	}
	session, returnTo, err := s.sessions.CompleteLogin(request.Context(), request.URL.Query().Get("state"), cookie.Value, request.URL.Query().Get("code"))
	if err != nil {
		writeError(response, status.Error(codes.Unauthenticated, "authentication failed"))
		return
	}
	maxAge := int(time.Until(session.Record.AbsoluteExpiresAt).Seconds())
	if maxAge < 1 {
		writeError(response, status.Error(codes.Unauthenticated, "authentication failed"))
		return
	}
	http.SetCookie(response, &http.Cookie{Name: adminSessionCookie, Value: session.SessionID, Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
	clearCookie(response, loginCookie)
	http.Redirect(response, request, authflow.SafeReturnPath(returnTo), http.StatusFound)
}

func (s *Server) logout(response http.ResponseWriter, request *http.Request) {
	session, err := s.authenticate(request)
	if err == nil {
		if err := s.requireMutation(request, session); err != nil {
			writeError(response, err)
			return
		}
		if err := s.sessions.Logout(request.Context(), session); err != nil {
			writeError(response, status.Error(codes.Unavailable, "logout could not be completed"))
			return
		}
	}
	clearCookie(response, adminSessionCookie)
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) currentSession(response http.ResponseWriter, request *http.Request) {
	session, err := s.authenticate(request)
	if err != nil {
		writeError(response, status.Error(codes.Unauthenticated, "authentication required"))
		return
	}
	type tenantView struct {
		ID          string            `json:"id"`
		Namespace   string            `json:"namespace"`
		DisplayName string            `json:"displayName"`
		Role        string            `json:"role"`
		Permissions []auth.Permission `json:"permissions"`
	}
	tenants := make([]tenantView, 0, len(session.Principal.Memberships))
	for _, membership := range session.Principal.Memberships {
		if !membership.Active || s.namespace != "" && membership.TenantNamespace != s.namespace {
			continue
		}
		permissions := membership.PermissionList()
		tenants = append(tenants, tenantView{ID: membership.TenantID, Namespace: membership.TenantNamespace,
			DisplayName: membership.TenantDisplayName, Role: string(membership.Role), Permissions: permissions})
	}
	sort.Slice(tenants, func(left, right int) bool { return tenants[left].Namespace < tenants[right].Namespace })
	writeJSON(response, http.StatusOK, map[string]any{
		"authenticated": true, "authMode": s.authMode, "principalId": session.Principal.ID,
		"csrfToken": session.Record.CSRFToken, "tenants": tenants,
	})
}

func (s *Server) listJobs(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if namespace := strings.TrimSpace(request.URL.Query().Get("namespace")); namespace != "" && namespace != scope.namespace {
		writeError(response, status.Error(codes.PermissionDenied, "tenant scope denied"))
		return
	}
	page, pageSize, err := parsePagination(request)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "pagination is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.jobs.ListJobs(ctx, &jobv1.ListJobsRequest{Namespace: scope.namespace, Page: int32(page), PageSize: int32(pageSize)})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) getJob(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	name := resourceName(request, "name")
	if name == "" {
		writeError(response, status.Error(codes.InvalidArgument, "job name is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.jobs.GetJob(ctx, &jobv1.GetJobRequest{Namespace: scope.namespace, Name: name})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) getJobStatus(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	name := resourceName(request, "name")
	if name == "" {
		writeError(response, status.Error(codes.InvalidArgument, "job name is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.jobs.GetJobStatus(ctx, &jobv1.GetJobStatusRequest{Namespace: scope.namespace, Name: name})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) listConnectors(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	_, pageSize, err := parsePagination(request)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "pagination is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.catalog.ListConnectorDescriptors(ctx, &jobv1.ListConnectorDescriptorsRequest{
		TenantId: scope.tenantID, PageSize: int32(pageSize), PageToken: request.URL.Query().Get("page_token"),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) getConnector(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	name := resourceName(request, "name")
	if name == "" || len(name) > 128 {
		writeError(response, status.Error(codes.InvalidArgument, "connector name is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.catalog.GetConnectorDescriptor(ctx, &jobv1.GetConnectorDescriptorRequest{
		TenantId: scope.tenantID, Name: name, DescriptorRevision: request.URL.Query().Get("descriptor_revision"),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

type scope struct {
	tenantID   string
	namespace  string
	membership auth.Membership
}

func (s *Server) scope(request *http.Request) (scope, authflow.Session, error) {
	session, err := s.authenticate(request)
	if err != nil {
		return scope{}, authflow.Session{}, status.Error(codes.Unauthenticated, "authentication required")
	}
	if s.legacy {
		membership, found := session.Principal.MembershipForScope(s.namespace)
		if !found {
			return scope{}, session, status.Error(codes.PermissionDenied, "tenant scope denied")
		}
		return scope{tenantID: membership.TenantID, namespace: s.namespace, membership: membership}, session, nil
	}
	requested := strings.TrimSpace(request.Header.Get("X-Astra-Tenant-ID"))
	var selected auth.Membership
	if requested != "" {
		membership, found := session.Principal.Memberships[requested]
		if !found || !membership.Active {
			return scope{}, session, status.Error(codes.PermissionDenied, "tenant scope denied")
		}
		selected = membership
	} else {
		candidates := make([]auth.Membership, 0, len(session.Principal.Memberships))
		for _, membership := range session.Principal.Memberships {
			if membership.Active && (s.namespace == "" || membership.TenantNamespace == s.namespace) {
				candidates = append(candidates, membership)
			}
		}
		if len(candidates) != 1 {
			return scope{}, session, status.Error(codes.InvalidArgument, "tenant selection is required")
		}
		selected = candidates[0]
	}
	if s.namespace != "" && selected.TenantNamespace != s.namespace {
		return scope{}, session, status.Error(codes.PermissionDenied, "tenant scope denied")
	}
	if selected.TenantID == "" || selected.TenantNamespace == "" {
		return scope{}, session, status.Error(codes.PermissionDenied, "tenant scope denied")
	}
	return scope{tenantID: selected.TenantID, namespace: selected.TenantNamespace, membership: selected}, session, nil
}

func (s *Server) authenticate(request *http.Request) (authflow.Session, error) {
	sessionID := ""
	if cookie, err := request.Cookie(adminSessionCookie); err == nil {
		sessionID = cookie.Value
	}
	return s.sessions.Resolve(request.Context(), sessionID)
}

func (s *Server) requireMutation(request *http.Request, session authflow.Session) error {
	if s.authMode == "oidc" {
		origin := strings.TrimRight(strings.TrimSpace(request.Header.Get("Origin")), "/")
		if origin == "" || origin != s.publicOrigin {
			return status.Error(codes.PermissionDenied, "same-origin request required")
		}
	}
	if !s.sessions.ValidateCSRF(session, request.Header.Get("X-CSRF-Token")) {
		return status.Error(codes.PermissionDenied, "CSRF validation failed")
	}
	contentType := strings.ToLower(request.Header.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "application/json") {
		return status.Error(codes.InvalidArgument, "JSON request body is required")
	}
	return nil
}

func (s *Server) backendContext(request *http.Request, session authflow.Session, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	requestID := strings.TrimSpace(request.Header.Get("X-Request-ID"))
	if requestID == "" || len(requestID) > 128 {
		requestID = uuid.NewString()
	}
	ctx = authflow.WithRequestID(ctx, requestID)
	if session.Record.Tokens.AccessToken != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+session.Record.Tokens.AccessToken,
			"x-request-id", requestID)
	}
	return ctx, cancel
}

func (s *Server) setScope(response http.ResponseWriter, selected scope) {
	response.Header().Set("X-Astra-Tenant-ID", selected.tenantID)
	response.Header().Set("X-Astra-Namespace", selected.namespace)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Referrer-Policy", "same-origin")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(request.URL.Path, "/api/") || strings.HasPrefix(request.URL.Path, "/auth/") {
			response.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(response, request)
	})
}

func parsePagination(request *http.Request) (int, int, error) {
	page, err := queryInt(request, "page", 1)
	if err != nil || page < 1 {
		return 0, 0, fmt.Errorf("page is invalid")
	}
	pageSize, err := queryInt(request, "page_size", defaultPageSize)
	if err != nil || pageSize < 1 || pageSize > maximumPageSize {
		return 0, 0, fmt.Errorf("page size is invalid")
	}
	return page, pageSize, nil
}

func queryInt(request *http.Request, key string, defaultValue int) (int, error) {
	value := request.URL.Query().Get(key)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func resourceName(request *http.Request, key string) string {
	value := strings.TrimSpace(request.PathValue(key))
	if len(value) > 253 || strings.ContainsAny(value, "/\r\n") {
		return ""
	}
	return value
}

func parseJSONBody(request *http.Request, destination any, maximum int64) error {
	if request.Body == nil {
		return fmt.Errorf("JSON request body is required")
	}
	limited := io.LimitReader(request.Body, maximum+1)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("request body must contain one JSON value")
	}
	return nil
}

func writeProtoJSON(response http.ResponseWriter, message proto.Message) {
	payload, err := (protojson.MarshalOptions{EmitUnpopulated: true, UseProtoNames: false}).Marshal(message)
	if err != nil {
		writeError(response, status.Error(codes.Internal, "response encoding failed"))
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(payload)
}

func writeJSON(response http.ResponseWriter, code int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(code)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, err error) {
	code := status.Code(err)
	if errors.Is(err, auth.ErrUnauthenticated) {
		code = codes.Unauthenticated
	}
	if errors.Is(err, auth.ErrSessionConflict) {
		code = codes.Aborted
	}
	statusCode := http.StatusInternalServerError
	message := "request failed"
	switch code {
	case codes.InvalidArgument:
		statusCode, message = http.StatusBadRequest, "request is invalid"
	case codes.Unauthenticated:
		statusCode, message = http.StatusUnauthorized, "authentication required"
	case codes.PermissionDenied:
		statusCode, message = http.StatusForbidden, "request is not authorized"
	case codes.NotFound:
		statusCode, message = http.StatusNotFound, "resource was not found"
	case codes.AlreadyExists, codes.Aborted:
		statusCode, message = http.StatusConflict, "resource changed or already exists"
	case codes.FailedPrecondition:
		statusCode, message = http.StatusPreconditionFailed, "request cannot be completed in the current state"
	case codes.ResourceExhausted:
		statusCode, message = http.StatusTooManyRequests, "request limit exceeded"
	case codes.Canceled, codes.DeadlineExceeded, codes.Unavailable:
		statusCode, message = http.StatusServiceUnavailable, "control plane is temporarily unavailable"
	}
	writeJSON(response, statusCode, map[string]string{"error": message, "code": code.String()})
}

func clearCookie(response http.ResponseWriter, name string) {
	http.SetCookie(response, &http.Cookie{Name: name, Value: "", Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func requestIDToken() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(value)
}
