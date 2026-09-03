package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JnyRoad/RelayDeck/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This catches accidentally sending the new login option through the legacy
// OAuth URL generator. A private-server login must return the official device
// verification details supplied by the app-server service.
func TestOpenAIOAuthHandler_StartAppServerDeviceCodeLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OpenAIOAuthHandler{
		appServerLoginService: &stubCodexAppServerLoginService{
			login: &service.CodexAppServerLogin{
				SessionID:       "app-server-session",
				LoginID:         "official-login",
				Mode:            service.CodexAppServerLoginModeDeviceCode,
				Status:          service.CodexAppServerLoginStatusPending,
				VerificationURL: "https://auth.openai.com/codex/device",
				UserCode:        "ABCD-1234",
			},
		},
	}
	router := gin.New()
	router.POST("/admin/openai/app-server/login/start", handler.StartAppServerLogin)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/openai/app-server/login/start",
		strings.NewReader(`{"mode":"device_code"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"verification_url":"https://auth.openai.com/codex/device"`)
	require.Contains(t, recorder.Body.String(), `"user_code":"ABCD-1234"`)
}

// This catches any regression that stores a provider token in RelayDeck when
// turning a completed official app-server login into an account. The account
// record must retain only the app-server profile reference and remain outside
// the legacy HTTP relay scheduler.
func TestOpenAIOAuthHandler_CreateAppServerAccountStoresOnlyProfileReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminService := newStubAdminService()
	handler := &OpenAIOAuthHandler{
		adminService: adminService,
		appServerLoginService: &stubCodexAppServerLoginService{
			login: &service.CodexAppServerLogin{
				SessionID: "app-server-session",
				Status:    service.CodexAppServerLoginStatusCompleted,
			},
		},
	}
	router := gin.New()
	router.POST("/admin/openai/app-server/login/:session_id/create-account", handler.CreateAppServerAccount)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/openai/app-server/login/app-server-session/create-account",
		strings.NewReader(`{"name":"个人官方运行时","concurrency":1,"priority":1}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, adminService.createdAccounts, 1)
	created := adminService.createdAccounts[0]
	require.Equal(t, service.PlatformOpenAI, created.Platform)
	require.Equal(t, service.AccountTypeOAuth, created.Type)
	require.Equal(t, "codex_app_server", created.Credentials["auth_provider"])
	require.Equal(t, "app-server-session", created.Credentials["app_server_profile_id"])
	require.NotContains(t, created.Credentials, "access_token")
	require.NotContains(t, created.Credentials, "refresh_token")
	require.NotNil(t, created.Schedulable)
	require.False(t, *created.Schedulable)
	require.True(t, created.SkipDefaultGroupBind)
}

func TestAccountHandler_RejectsSchedulingManagedAppServerProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminService := newStubAdminService()
	adminService.getAccountResult = &service.Account{
		ID:          9,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"auth_provider": "codex_app_server"},
	}
	handler := &AccountHandler{adminService: adminService}
	router := gin.New()
	router.POST("/admin/accounts/:id/schedulable", handler.SetSchedulable)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/accounts/9/schedulable", strings.NewReader(`{"schedulable":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "官方 app-server")
}

type stubCodexAppServerLoginService struct {
	login *service.CodexAppServerLogin
}

func (s *stubCodexAppServerLoginService) StartLogin(_ context.Context, _ service.CodexAppServerLoginMode) (*service.CodexAppServerLogin, error) {
	return s.login, nil
}

func (s *stubCodexAppServerLoginService) GetLogin(_ string) (*service.CodexAppServerLogin, error) {
	return s.login, nil
}

func (s *stubCodexAppServerLoginService) CompleteLogin(_ string) (string, error) {
	return s.login.SessionID, nil
}

func (s *stubCodexAppServerLoginService) FinalizeLogin(_ string) error { return nil }

func (s *stubCodexAppServerLoginService) CancelLogin(_ context.Context, _ string) error { return nil }
