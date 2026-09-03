package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/handler/dto"
	"github.com/JnyRoad/RelayDeck/internal/pkg/openai"
	"github.com/JnyRoad/RelayDeck/internal/pkg/response"
	"github.com/JnyRoad/RelayDeck/internal/service"

	"github.com/gin-gonic/gin"
)

// OpenAIOAuthHandler handles OpenAI OAuth-related operations
type OpenAIOAuthHandler struct {
	openaiOAuthService    *service.OpenAIOAuthService
	adminService          service.AdminService
	quotaService          openAIQuotaService
	rateLimitService      openAIAccountStateRecoverer
	appServerLoginService codexAppServerLoginService
}

// codexAppServerLoginService keeps the official app-server protocol behind a
// narrow handler boundary. It intentionally exposes login state, never tokens.
type codexAppServerLoginService interface {
	StartLogin(ctx context.Context, mode service.CodexAppServerLoginMode) (*service.CodexAppServerLogin, error)
	GetLogin(sessionID string) (*service.CodexAppServerLogin, error)
	CompleteLogin(sessionID string) (string, error)
	FinalizeLogin(sessionID string) error
	CancelLogin(ctx context.Context, sessionID string) error
	TransportKind() string
}

type openAIQuotaService interface {
	QueryUsage(ctx context.Context, accountID int64) (*service.OpenAIQuotaUsage, error)
	CacheResetCreditsSnapshot(ctx context.Context, accountID int64, credits *service.OpenAIRateLimitResetCredits) error
	CachePostResetSnapshot(ctx context.Context, accountID int64, usage *service.OpenAIQuotaUsage) error
	ResetCredit(ctx context.Context, accountID int64) (*service.OpenAIQuotaResetResult, error)
}

type openAIAccountStateRecoverer interface {
	RecoverAccountState(ctx context.Context, accountID int64, options service.AccountRecoveryOptions) (*service.SuccessfulTestRecoveryResult, error)
}

// openAIQuotaResetPostProcessTimeout bounds the work performed AFTER the
// (non-refundable) reset credit has already been consumed upstream. The whole
// request must stay comfortably inside the panel HTTP client timeout, otherwise
// the browser aborts a mutation that already succeeded and the operator retries
// it — spending a second credit.
const openAIQuotaResetPostProcessTimeout = 8 * time.Second

type openAIQuotaResetResponse struct {
	service.OpenAIQuotaResetResult
	Quota                 *service.OpenAIQuotaUsage `json:"quota,omitempty"`
	Account               *dto.Account              `json:"account,omitempty"`
	CacheRefreshed        bool                      `json:"cache_refreshed"`
	AccountStateRecovered bool                      `json:"account_state_recovered"`
	WarningCode           string                    `json:"warning_code,omitempty"`
}

// openAIQuotaRefreshResponse is the reset-credit-persisting variant of the quota
// query. The usage payload is embedded so the shape stays identical to the plain
// query; cache_persisted reports whether the snapshot write succeeded, because a
// failed display-cache write must never discard a successful upstream read.
type openAIQuotaRefreshResponse struct {
	service.OpenAIQuotaUsage
	CachePersisted bool `json:"cache_persisted"`
}

// openAIQuotaResetPostProcessContext detaches the post-reset bookkeeping from the
// client connection. The credit is already spent at that point, so account-state
// recovery must complete even if the operator closes the tab (mirrors
// systemUpdateContext, added for the same reason in #4504).
func openAIQuotaResetPostProcessContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, openAIQuotaResetPostProcessTimeout)
}

func oauthPlatformFromPath(c *gin.Context) string {
	return service.PlatformOpenAI
}

// NewOpenAIOAuthHandler creates a new OpenAI OAuth handler
func NewOpenAIOAuthHandler(
	openaiOAuthService *service.OpenAIOAuthService,
	adminService service.AdminService,
	quotaService *service.OpenAIQuotaService,
	rateLimitService *service.RateLimitService,
) *OpenAIOAuthHandler {
	h := &OpenAIOAuthHandler{
		openaiOAuthService:    openaiOAuthService,
		adminService:          adminService,
		appServerLoginService: service.NewCodexAppServerService(service.CodexAppServerServiceConfig{}),
	}
	// Assign through explicit nil checks: storing a nil *Service in an interface
	// field yields a non-nil interface, which would silently defeat the
	// `== nil` capability guards below and panic instead of returning 400.
	if quotaService != nil {
		h.quotaService = quotaService
	}
	if rateLimitService != nil {
		h.rateLimitService = rateLimitService
	}
	return h
}

type startCodexAppServerLoginRequest struct {
	Mode service.CodexAppServerLoginMode `json:"mode"`
}

// StartAppServerLogin starts a login owned by the official Codex app-server.
// POST /api/v1/admin/openai/app-server/login/start
func (h *OpenAIOAuthHandler) StartAppServerLogin(c *gin.Context) {
	if h.appServerLoginService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex app-server 登录服务不可用")
		return
	}
	var req startCodexAppServerLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if strings.TrimSpace(string(req.Mode)) == "" {
		req.Mode = service.CodexAppServerLoginModeDeviceCode
	}
	login, err := h.appServerLoginService.StartLogin(c.Request.Context(), req.Mode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, login)
}

// GetAppServerLogin returns the non-secret progress of an official app-server
// login. The private-server UI polls this endpoint; no provider callback is
// received by RelayDeck.
// GET /api/v1/admin/openai/app-server/login/:session_id
func (h *OpenAIOAuthHandler) GetAppServerLogin(c *gin.Context) {
	if h.appServerLoginService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex app-server 登录服务不可用")
		return
	}
	login, err := h.appServerLoginService.GetLogin(c.Param("session_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, login)
}

// CancelAppServerLogin cancels an incomplete official app-server login and
// deletes its isolated profile directory.
// POST /api/v1/admin/openai/app-server/login/:session_id/cancel
func (h *OpenAIOAuthHandler) CancelAppServerLogin(c *gin.Context) {
	if h.appServerLoginService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex app-server 登录服务不可用")
		return
	}
	if err := h.appServerLoginService.CancelLogin(c.Request.Context(), c.Param("session_id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"status": "cancelled"})
}

type createCodexAppServerAccountRequest struct {
	Name     string  `json:"name" binding:"required"`
	Notes    *string `json:"notes"`
	Priority int     `json:"priority"`
}

// CreateAppServerAccount persists only a reference to an already-authenticated
// official app-server profile. OAuth tokens and the profile filesystem path do
// not enter RelayDeck's database or HTTP API.
// POST /api/v1/admin/openai/app-server/login/:session_id/create-account
func (h *OpenAIOAuthHandler) CreateAppServerAccount(c *gin.Context) {
	if h.appServerLoginService == nil || h.adminService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex app-server 登录服务不可用")
		return
	}
	var req createCodexAppServerAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.BadRequest(c, "name is required")
		return
	}
	if req.Priority < 0 {
		response.BadRequest(c, "priority must be >= 0")
		return
	}

	profileID, err := h.appServerLoginService.CompleteLogin(c.Param("session_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	notSchedulable := false
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:     req.Name,
		Notes:    req.Notes,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_provider":         "codex_app_server",
			"app_server_profile_id": profileID,
		},
		Extra: map[string]any{
			"app_server_managed":   true,
			"app_server_transport": h.appServerLoginService.TransportKind(),
		},
		Concurrency:           1,
		Priority:              req.Priority,
		Schedulable:           &notSchedulable,
		SkipDefaultGroupBind:  true,
		SkipMixedChannelCheck: true,
	})
	if err != nil {
		// No account references this completed profile, so it must be released
		// before returning a persistence error to the browser.
		if cancelErr := h.appServerLoginService.CancelLogin(c.Request.Context(), c.Param("session_id")); cancelErr != nil {
			slog.Warn("release_codex_app_server_login_failed", "profile_id", profileID, "error", cancelErr)
		}
		response.ErrorFrom(c, err)
		return
	}
	if err := h.appServerLoginService.FinalizeLogin(c.Param("session_id")); err != nil {
		// The account is already safely persisted. Keep the error in logs for
		// operator cleanup rather than encouraging a duplicate account creation.
		slog.Warn("finalize_codex_app_server_login_failed", "profile_id", profileID, "error", err)
	}
	response.Success(c, dto.AccountFromService(account))
}

// OpenAIGenerateAuthURLRequest represents the request for generating OpenAI auth URL
type OpenAIGenerateAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

// GenerateAuthURL generates OpenAI OAuth authorization URL
// POST /api/v1/admin/openai/generate-auth-url
func (h *OpenAIOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req OpenAIGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body
		req = OpenAIGenerateAuthURLRequest{}
	}

	result, err := h.openaiOAuthService.GenerateAuthURL(
		c.Request.Context(),
		req.ProxyID,
		req.RedirectURI,
		oauthPlatformFromPath(c),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// OpenAIExchangeCodeRequest represents the request for exchanging OpenAI auth code
type OpenAIExchangeCodeRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	State       string `json:"state" binding:"required"`
	RedirectURI string `json:"redirect_uri"`
	ProxyID     *int64 `json:"proxy_id"`
}

// ExchangeCode exchanges OpenAI authorization code for tokens
// POST /api/v1/admin/openai/exchange-code
func (h *OpenAIOAuthHandler) ExchangeCode(c *gin.Context) {
	var req OpenAIExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	tokenInfo, err := h.openaiOAuthService.ExchangeCode(c.Request.Context(), &service.OpenAIExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, tokenInfo)
}

// OpenAIRefreshTokenRequest represents the request for refreshing OpenAI token
type OpenAIRefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
	RT           string `json:"rt"`
	ClientID     string `json:"client_id"`
	ProxyID      *int64 `json:"proxy_id"`
}

type OpenAICodexPATCreateRequest struct {
	AccessToken             string         `json:"access_token" binding:"required"`
	Name                    string         `json:"name"`
	Notes                   *string        `json:"notes"`
	GroupIDs                []int64        `json:"group_ids"`
	ProxyID                 *int64         `json:"proxy_id"`
	Concurrency             *int           `json:"concurrency"`
	Priority                *int           `json:"priority"`
	RateMultiplier          *float64       `json:"rate_multiplier"`
	LoadFactor              *int           `json:"load_factor"`
	ExpiresAt               *int64         `json:"expires_at"`
	AutoPauseOnExpired      *bool          `json:"auto_pause_on_expired"`
	CredentialExtras        map[string]any `json:"credential_extras"`
	Extra                   map[string]any `json:"extra"`
	SkipDefaultGroupBind    *bool          `json:"skip_default_group_bind"`
	ConfirmMixedChannelRisk *bool          `json:"confirm_mixed_channel_risk"`
}

// RefreshToken refreshes an OpenAI OAuth token
// POST /api/v1/admin/openai/refresh-token
func (h *OpenAIOAuthHandler) RefreshToken(c *gin.Context) {
	var req OpenAIRefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(req.RT)
	}
	if refreshToken == "" {
		response.BadRequest(c, "refresh_token is required")
		return
	}

	var proxyURL string
	if req.ProxyID != nil {
		proxy, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// 未指定 client_id 时，根据请求路径平台自动设置默认值，避免 repository 层盲猜
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		platform := oauthPlatformFromPath(c)
		clientID, _ = openai.OAuthClientConfigByPlatform(platform)
	}

	tokenInfo, err := h.openaiOAuthService.RefreshTokenWithClientID(c.Request.Context(), refreshToken, proxyURL, clientID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, tokenInfo)
}

// RefreshAccountToken refreshes token for a specific OpenAI account
// POST /api/v1/admin/openai/accounts/:id/refresh
func (h *OpenAIOAuthHandler) RefreshAccountToken(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	// Get account
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	platform := oauthPlatformFromPath(c)
	if account.Platform != platform {
		response.BadRequest(c, "Account platform does not match OAuth endpoint")
		return
	}

	// Only refresh OAuth-based accounts
	if !account.IsOAuth() {
		response.BadRequest(c, "Cannot refresh non-OAuth account credentials")
		return
	}
	if account.IsCodexAppServerManaged() {
		response.BadRequest(c, "该账号由官方 app-server 管理，请在其运行时中处理登录状态")
		return
	}

	// spark 影子账号凭据透传母账号、自身恒空,刷新无意义;在调用上游前早拒,避免先打上游
	// 再被凭据写守卫拦下的无谓副作用(外审第6轮)。
	if account.IsCredentialShadow() {
		response.BadRequest(c, "Cannot refresh spark shadow account; its credentials are managed by the parent account")
		return
	}

	// Use OpenAI OAuth service to refresh token
	tokenInfo, err := h.openaiOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Build new credentials from token info
	newCredentials := h.openaiOAuthService.BuildAccountCredentials(tokenInfo)

	// Preserve non-token settings from existing credentials
	for k, v := range account.Credentials {
		if _, exists := newCredentials[k]; !exists {
			newCredentials[k] = v
		}
	}
	newCredentials = service.NormalizeOpenAIPersonalAccessTokenCredentials(account, tokenInfo, newCredentials)

	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(updatedAccount))
}

// CreateAccountFromOAuth creates a new OpenAI OAuth account from token info
// POST /api/v1/admin/openai/create-from-oauth
func (h *OpenAIOAuthHandler) CreateAccountFromOAuth(c *gin.Context) {
	var req struct {
		SessionID   string  `json:"session_id" binding:"required"`
		Code        string  `json:"code" binding:"required"`
		State       string  `json:"state" binding:"required"`
		RedirectURI string  `json:"redirect_uri"`
		ProxyID     *int64  `json:"proxy_id"`
		Name        string  `json:"name"`
		Concurrency int     `json:"concurrency"`
		Priority    int     `json:"priority"`
		GroupIDs    []int64 `json:"group_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Exchange code for tokens
	tokenInfo, err := h.openaiOAuthService.ExchangeCode(c.Request.Context(), &service.OpenAIExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Build credentials from token info
	credentials := h.openaiOAuthService.BuildAccountCredentials(tokenInfo)

	platform := oauthPlatformFromPath(c)

	// Use email as default name if not provided
	name := req.Name
	if name == "" && tokenInfo.Email != "" {
		name = tokenInfo.Email
	}
	if name == "" {
		name = "OpenAI OAuth Account"
	}

	// Create account
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:        name,
		Platform:    platform,
		Type:        "oauth",
		Credentials: credentials,
		Extra:       nil,
		ProxyID:     req.ProxyID,
		Concurrency: req.Concurrency,
		Priority:    req.Priority,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(account))
}

// CreateAccountFromCodexPAT creates an OpenAI OAuth account from a Codex at-* personal access token.
// POST /api/v1/admin/openai/create-from-codex-pat
func (h *OpenAIOAuthHandler) CreateAccountFromCodexPAT(c *gin.Context) {
	var req OpenAICodexPATCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.ValidateOpenAILongContextBillingExtra(service.PlatformOpenAI, req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if req.Concurrency != nil && *req.Concurrency < 0 {
		response.BadRequest(c, "concurrency must be >= 0")
		return
	}
	if req.Priority != nil && *req.Priority < 0 {
		response.BadRequest(c, "priority must be >= 0")
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	if req.LoadFactor != nil && *req.LoadFactor > 10000 {
		response.BadRequest(c, "load_factor must be <= 10000")
		return
	}

	var proxyURL string
	if req.ProxyID != nil {
		proxy, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	tokenInfo, err := h.openaiOAuthService.ValidateCodexPersonalAccessToken(c.Request.Context(), req.AccessToken, proxyURL)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	credentials := mergeCodexImportMap(
		h.openaiOAuthService.BuildAccountCredentials(tokenInfo),
		sanitizeCodexImportCredentialExtras(req.CredentialExtras),
	)
	extra := mergeCodexImportMap(req.Extra, map[string]any{
		"import_source":       "codex_personal_access_token",
		"auth_provider":       "codex_personal_access_token",
		"imported_at":         time.Now().UTC().Format(time.RFC3339),
		"access_token_sha256": codexTokenFingerprint(req.AccessToken),
	})

	concurrency := 3
	if req.Concurrency != nil {
		concurrency = *req.Concurrency
	}
	priority := 50
	if req.Priority != nil {
		priority = *req.Priority
	}
	skipDefaultGroupBind := false
	if req.SkipDefaultGroupBind != nil {
		skipDefaultGroupBind = *req.SkipDefaultGroupBind
	}

	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:                  buildOpenAICodexPATAccountName(req.Name, tokenInfo),
		Notes:                 req.Notes,
		Platform:              service.PlatformOpenAI,
		Type:                  service.AccountTypeOAuth,
		Credentials:           credentials,
		Extra:                 extra,
		ProxyID:               req.ProxyID,
		Concurrency:           concurrency,
		Priority:              priority,
		RateMultiplier:        req.RateMultiplier,
		LoadFactor:            req.LoadFactor,
		GroupIDs:              req.GroupIDs,
		ExpiresAt:             req.ExpiresAt,
		AutoPauseOnExpired:    req.AutoPauseOnExpired,
		SkipDefaultGroupBind:  skipDefaultGroupBind,
		SkipMixedChannelCheck: req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(account))
}

func buildOpenAICodexPATAccountName(name string, tokenInfo *service.OpenAITokenInfo) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	if tokenInfo != nil {
		for _, candidate := range []string{tokenInfo.Email, tokenInfo.ChatGPTAccountID, tokenInfo.ChatGPTUserID} {
			if candidate = strings.TrimSpace(candidate); candidate != "" {
				return candidate
			}
		}
	}
	return "Codex PAT Account"
}

// QueryQuota queries the rate-limit / quota usage for an OpenAI account.
// GET /api/v1/admin/openai/accounts/:id/quota
func (h *OpenAIOAuthHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}

	usage, err := h.quotaService.QueryUsage(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	service.NotifyOpenAIAutoResetCredit(accountID)
	response.Success(c, usage)
}

// RefreshQuota queries the rate-limit / quota usage AND persists the reset-credit
// snapshot so the card can be rehydrated without an upstream round-trip.
// POST /api/v1/admin/openai/accounts/:id/quota/refresh
//
// It is a POST (not a GET with a side-effect flag) because it writes account
// state: the audit middleware only records mutating verbs, so a persisting GET
// would mutate the database without an audit trail.
func (h *OpenAIOAuthHandler) RefreshQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}

	usage, err := h.quotaService.QueryUsage(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if usage == nil {
		response.Error(c, http.StatusInternalServerError, "openai quota query returned an empty result")
		return
	}
	service.NotifyOpenAIAutoResetCredit(accountID)

	refreshResponse := openAIQuotaRefreshResponse{OpenAIQuotaUsage: *usage}
	// A failed snapshot write leaves the previous cache intact — report it as a
	// partial success instead of discarding the usage payload we just fetched,
	// which would leave the card without a credit count at all.
	if err := h.quotaService.CacheResetCreditsSnapshot(c.Request.Context(), accountID, usage.RateLimitResetCredits); err != nil {
		slog.Warn("openai_quota_reset_credit_cache_persist_failed", "account_id", accountID, "error", err)
		response.Success(c, refreshResponse)
		return
	}
	refreshResponse.CachePersisted = true
	response.Success(c, refreshResponse)
}

// CreateShadowRequest is the request body for CreateShadow.
type CreateShadowRequest struct {
	Name        string  `json:"name"`
	Priority    int     `json:"priority"`
	Concurrency int     `json:"concurrency"`
	GroupIDs    []int64 `json:"group_ids"`
}

// CreateShadow creates a spark-dimension shadow account for a parent OpenAI OAuth account.
// POST /api/v1/admin/accounts/:id/shadow
func (h *OpenAIOAuthHandler) CreateShadow(c *gin.Context) {
	parentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req CreateShadowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	shadow, err := h.adminService.CreateShadow(c.Request.Context(), parentID, service.ShadowOptions{
		Name:        req.Name,
		Priority:    req.Priority,
		Concurrency: req.Concurrency,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromServiceShallow(shadow))
}

// ResetQuota consumes one rate-limit reset credit for an OpenAI account.
// POST /api/v1/admin/openai/accounts/:id/reset-quota
func (h *OpenAIOAuthHandler) ResetQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}
	result, err := h.quotaService.ResetCredit(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result == nil {
		response.Error(c, http.StatusInternalServerError, "openai quota reset returned an empty result")
		return
	}

	resetResponse := openAIQuotaResetResponse{OpenAIQuotaResetResult: *result}
	postCtx, cancelPost := openAIQuotaResetPostProcessContext(c.Request.Context())
	defer cancelPost()

	postResult := service.RunOpenAIQuotaResetPostProcess(
		postCtx,
		accountID,
		h.quotaService,
		h.rateLimitService,
		h.adminService.GetAccount,
	)
	resetResponse.Quota = postResult.Quota
	resetResponse.CacheRefreshed = postResult.CacheRefreshed
	resetResponse.AccountStateRecovered = postResult.AccountStateRecovered
	resetResponse.WarningCode = postResult.WarningCode
	if postResult.Account != nil {
		resetResponse.Account = dto.AccountFromService(postResult.Account)
	}
	response.Success(c, resetResponse)
}
