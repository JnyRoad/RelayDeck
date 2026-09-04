package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/pkg/pagination"
	"github.com/JnyRoad/RelayDeck/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// adminUserAPIKeyManagerStub records target-scoped Key operations for route tests.
type adminUserAPIKeyManagerStub struct {
	keys       []service.APIKey
	pagination *pagination.PaginationResult
	groups     []service.Group
	rates      map[int64]float64

	listUserID     int64
	listParams     pagination.PaginationParams
	listFilters    service.APIKeyListFilters
	createUserID   int64
	createRequest  service.CreateAPIKeyRequest
	updateUserID   int64
	updateKeyID    int64
	updateRequest  service.UpdateAPIKeyRequest
	updateErr      error
	deleteUserID   int64
	deleteKeyID    int64
	availableForID int64
	ratesForID     int64
}

// List returns configured records while capturing the target-scoped query.
func (s *adminUserAPIKeyManagerStub) List(_ context.Context, userID int64, params pagination.PaginationParams, filters service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	s.listUserID = userID
	s.listParams = params
	s.listFilters = filters
	return s.keys, s.pagination, nil
}

// Create captures the target user and returns a representative created Key.
func (s *adminUserAPIKeyManagerStub) Create(_ context.Context, userID int64, req service.CreateAPIKeyRequest) (*service.APIKey, error) {
	s.createUserID = userID
	s.createRequest = req
	return &service.APIKey{ID: 21, UserID: userID, Key: "sk-admin-target", Name: req.Name, Status: service.StatusAPIKeyActive}, nil
}

// Update captures the ownership-scoped mutation and can simulate a forbidden Key.
func (s *adminUserAPIKeyManagerStub) Update(_ context.Context, keyID int64, userID int64, req service.UpdateAPIKeyRequest) (*service.APIKey, error) {
	s.updateKeyID = keyID
	s.updateUserID = userID
	s.updateRequest = req
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return &service.APIKey{ID: keyID, UserID: userID, Key: "sk-admin-target", Status: service.StatusAPIKeyActive}, nil
}

// Delete captures the ownership-scoped Key removal.
func (s *adminUserAPIKeyManagerStub) Delete(_ context.Context, keyID int64, userID int64) error {
	s.deleteKeyID = keyID
	s.deleteUserID = userID
	return nil
}

// GetAvailableGroups returns the groups eligible for the supplied target user.
func (s *adminUserAPIKeyManagerStub) GetAvailableGroups(_ context.Context, userID int64) ([]service.Group, error) {
	s.availableForID = userID
	return s.groups, nil
}

// GetUserGroupRates returns target-specific group rate overrides.
func (s *adminUserAPIKeyManagerStub) GetUserGroupRates(_ context.Context, userID int64) (map[int64]float64, error) {
	s.ratesForID = userID
	return s.rates, nil
}

// setupAdminUserAPIKeyRouter exposes only the target-user Key endpoints under test.
func setupAdminUserAPIKeyRouter(t *testing.T, manager *adminUserAPIKeyManagerStub, adminSvc *stubAdminService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	handler.SetAPIKeyManager(manager)
	router := gin.New()
	router.GET("/api/v1/admin/users/:id/api-keys", handler.GetUserAPIKeys)
	router.POST("/api/v1/admin/users/:id/api-keys", handler.CreateUserAPIKey)
	router.PUT("/api/v1/admin/users/:id/api-keys/:key_id", handler.UpdateUserAPIKey)
	router.DELETE("/api/v1/admin/users/:id/api-keys/:key_id", handler.DeleteUserAPIKey)
	router.GET("/api/v1/admin/users/:id/api-keys/available-groups", handler.GetUserAPIKeyAvailableGroups)
	router.GET("/api/v1/admin/users/:id/api-keys/group-rates", handler.GetUserAPIKeyGroupRates)
	return router
}

// TestAdminUserAPIKeyListForwardsTargetAndFilters verifies a list never falls back to the administrator owner.
func TestAdminUserAPIKeyListForwardsTargetAndFilters(t *testing.T) {
	now := time.Now().UTC()
	manager := &adminUserAPIKeyManagerStub{
		keys:       []service.APIKey{{ID: 11, UserID: 1, Key: "sk-admin-target", Name: "target", Status: service.StatusAPIKeyActive, CreatedAt: now, UpdatedAt: now}},
		pagination: &pagination.PaginationResult{Total: 1, Page: 2, PageSize: 30, Pages: 1},
	}
	router := setupAdminUserAPIKeyRouter(t, manager, newStubAdminService())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1/api-keys?page=2&page_size=30&search=target&status=active&group_id=3&sort_by=name&sort_order=asc", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, int64(1), manager.listUserID)
	require.Equal(t, pagination.PaginationParams{Page: 2, PageSize: 30, SortBy: "name", SortOrder: "asc"}, manager.listParams)
	require.Equal(t, "target", manager.listFilters.Search)
	require.Equal(t, service.StatusAPIKeyActive, manager.listFilters.Status)
	require.NotNil(t, manager.listFilters.GroupID)
	require.Equal(t, int64(3), *manager.listFilters.GroupID)
	require.Contains(t, recorder.Body.String(), "sk-admin-target")
}

// TestAdminUserAPIKeyMutationsUseTargetOwner verifies create, reset/update, and delete carry the path owner.
func TestAdminUserAPIKeyMutationsUseTargetOwner(t *testing.T) {
	manager := &adminUserAPIKeyManagerStub{}
	router := setupAdminUserAPIKeyRouter(t, manager, newStubAdminService())

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/1/api-keys", bytes.NewBufferString(`{"name":"assigned","quota":12.5}`))
	createRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(createRecorder, createRequest)
	require.Equal(t, http.StatusOK, createRecorder.Code, createRecorder.Body.String())
	require.Equal(t, int64(1), manager.createUserID)
	require.Equal(t, "assigned", manager.createRequest.Name)
	require.Equal(t, 12.5, manager.createRequest.Quota)

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1/api-keys/21", bytes.NewBufferString(`{"reset_quota":true,"reset_rate_limit_usage":true}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(updateRecorder, updateRequest)
	require.Equal(t, http.StatusOK, updateRecorder.Code, updateRecorder.Body.String())
	require.Equal(t, int64(1), manager.updateUserID)
	require.Equal(t, int64(21), manager.updateKeyID)
	require.NotNil(t, manager.updateRequest.ResetQuota)
	require.True(t, *manager.updateRequest.ResetQuota)
	require.NotNil(t, manager.updateRequest.ResetRateLimitUsage)
	require.True(t, *manager.updateRequest.ResetRateLimitUsage)

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/1/api-keys/21", nil)
	router.ServeHTTP(deleteRecorder, deleteRequest)
	require.Equal(t, http.StatusOK, deleteRecorder.Code, deleteRecorder.Body.String())
	require.Equal(t, int64(1), manager.deleteUserID)
	require.Equal(t, int64(21), manager.deleteKeyID)
}

// TestAdminUserAPIKeyRejectsUnavailableTargetAndCrossUserKey verifies target existence and service ownership failures are retained.
func TestAdminUserAPIKeyRejectsUnavailableTargetAndCrossUserKey(t *testing.T) {
	t.Run("unavailable target", func(t *testing.T) {
		adminSvc := newStubAdminService()
		adminSvc.getUserErr = service.ErrUserNotFound
		router := setupAdminUserAPIKeyRouter(t, &adminUserAPIKeyManagerStub{}, adminSvc)

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/999/api-keys", nil)
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
	})

	t.Run("disabled target cannot receive a new key", func(t *testing.T) {
		adminSvc := newStubAdminService()
		adminSvc.users[0].Status = service.StatusDisabled
		manager := &adminUserAPIKeyManagerStub{}
		router := setupAdminUserAPIKeyRouter(t, manager, adminSvc)

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/1/api-keys", bytes.NewBufferString(`{"name":"assigned"}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
		require.Zero(t, manager.createUserID)
	})

	t.Run("disabled target cannot mutate existing keys", func(t *testing.T) {
		adminSvc := newStubAdminService()
		adminSvc.users[0].Status = service.StatusDisabled
		manager := &adminUserAPIKeyManagerStub{}
		router := setupAdminUserAPIKeyRouter(t, manager, adminSvc)

		updateRecorder := httptest.NewRecorder()
		updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1/api-keys/21", bytes.NewBufferString(`{"status":"inactive"}`))
		updateRequest.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(updateRecorder, updateRequest)
		require.Equal(t, http.StatusForbidden, updateRecorder.Code, updateRecorder.Body.String())
		require.Zero(t, manager.updateUserID)

		deleteRecorder := httptest.NewRecorder()
		deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/1/api-keys/21", nil)
		router.ServeHTTP(deleteRecorder, deleteRequest)
		require.Equal(t, http.StatusForbidden, deleteRecorder.Code, deleteRecorder.Body.String())
		require.Zero(t, manager.deleteUserID)
	})

	t.Run("key belongs to another user", func(t *testing.T) {
		manager := &adminUserAPIKeyManagerStub{updateErr: service.ErrInsufficientPerms}
		router := setupAdminUserAPIKeyRouter(t, manager, newStubAdminService())

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1/api-keys/999", bytes.NewBufferString(`{"status":"inactive"}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	})
}

// TestAdminUserAPIKeyMetadataUsesTargetOwner verifies group and rate lookups use the selected user.
func TestAdminUserAPIKeyMetadataUsesTargetOwner(t *testing.T) {
	manager := &adminUserAPIKeyManagerStub{
		groups: []service.Group{{ID: 7, Name: "eligible", Status: service.StatusActive}},
		rates:  map[int64]float64{7: 1.25},
	}
	router := setupAdminUserAPIKeyRouter(t, manager, newStubAdminService())

	groupsRecorder := httptest.NewRecorder()
	groupsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1/api-keys/available-groups", nil)
	router.ServeHTTP(groupsRecorder, groupsRequest)
	require.Equal(t, http.StatusOK, groupsRecorder.Code, groupsRecorder.Body.String())
	require.Equal(t, int64(1), manager.availableForID)

	ratesRecorder := httptest.NewRecorder()
	ratesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1/api-keys/group-rates", nil)
	router.ServeHTTP(ratesRecorder, ratesRequest)
	require.Equal(t, http.StatusOK, ratesRecorder.Code, ratesRecorder.Body.String())
	require.Equal(t, int64(1), manager.ratesForID)

	var body map[string]any
	require.NoError(t, json.Unmarshal(ratesRecorder.Body.Bytes(), &body))
	require.Contains(t, body["data"], "7")
}
