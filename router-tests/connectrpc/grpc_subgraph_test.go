package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	projects "github.com/wundergraph/cosmo/demo/pkg/subgraphs/projects/generated"
	projectsservice "github.com/wundergraph/cosmo/demo/pkg/subgraphs/projects/src/service"
	"github.com/wundergraph/cosmo/router-tests/testenv"
	"github.com/wundergraph/cosmo/router-tests/testutils"
	"github.com/wundergraph/cosmo/router/core"
	nodev1 "github.com/wundergraph/cosmo/router/gen/proto/wg/cosmo/node/v1"
	"github.com/wundergraph/cosmo/router/pkg/config"
)

func TestConnectRPCGRPCSubgraph(t *testing.T) {
	for _, encoding := range []string{"proto", "json"} {
		t.Run(encoding, func(t *testing.T) {
			var (
				capturedMu          sync.Mutex
				capturedContentType []string
				capturedPaths       []string
				capturedHeaders     http.Header
			)

			server := newProjectsConnectServer(t, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					capturedMu.Lock()
					capturedContentType = append(capturedContentType, r.Header.Get("Content-Type"))
					capturedPaths = append(capturedPaths, r.URL.Path)
					if r.Header.Get("X-Tenant-Id") != "" {
						capturedHeaders = r.Header.Clone()
					}
					capturedMu.Unlock()
					next.ServeHTTP(w, r)
				})
			})

			testenv.Run(t, connectRPCSubgraphTestConfig(server.URL+"/rpc/", encoding,
				core.WithHeaderRules(config.HeaderRules{
					All: &config.GlobalHeaderRule{Request: []*config.RequestHeaderRule{
						{Operation: config.HeaderRuleOperationPropagate, Named: "X-Tenant-Id"},
						{Operation: config.HeaderRuleOperationPropagate, Named: "X-Token-Bin"},
					}},
				}),
			), func(t *testing.T, xEnv *testenv.Environment) {
				queryResponse := xEnv.MakeGraphQLRequestOK(testenv.GraphQLRequest{
					Query: `query { project(id: 1) { id name description status } }`,
					Header: http.Header{
						"X-Tenant-Id": []string{"acme"},
						"X-Token-Bin": []string{"raw-binary"},
					},
				})
				assert.Equal(t, `{"data":{"project":{"id":"1","name":"Cloud Migration Overhaul","description":"Migrate legacy systems to cloud-native architecture","status":"ACTIVE"}}}`, queryResponse.Body)

				entityResponse := xEnv.MakeGraphQLRequestOK(testenv.GraphQLRequest{
					Query: `query { employee(id: 1) { id projects { id name } } }`,
				})
				assert.Equal(t, `{"data":{"employee":{"id":1,"projects":[{"id":"1","name":"Cloud Migration Overhaul"},{"id":"4","name":"DevOps Transformation"}]}}}`, entityResponse.Body)

				resolverResponse := xEnv.MakeGraphQLRequestOK(testenv.GraphQLRequest{
					Query: `query { project(id: 1) { filteredTasks(limit: 2) { name status } } }`,
				})
				assert.Equal(t, `{"data":{"project":{"filteredTasks":[{"name":"Current Infrastructure Audit","status":"COMPLETED"},{"name":"Cloud Provider Selection","status":"COMPLETED"}]}}}`, resolverResponse.Body)

				mutationResponse := xEnv.MakeGraphQLRequestOK(testenv.GraphQLRequest{
					Query: `mutation { updateProjectStatus(projectId: "1", status: COMPLETED) { id projectId updateType } }`,
				})
				assert.Equal(t, `{"data":{"updateProjectStatus":{"id":"connect-update","projectId":"1","updateType":"STATUS_CHANGE"}}}`, mutationResponse.Body)
			})

			capturedMu.Lock()
			defer capturedMu.Unlock()
			require.NotEmpty(t, capturedContentType)
			for _, contentType := range capturedContentType {
				assert.Equal(t, "application/"+encoding, contentType)
			}
			for _, path := range capturedPaths {
				assert.True(t, strings.HasPrefix(path, "/service.ProjectsService/"), path)
			}
			assert.Equal(t, "acme", capturedHeaders.Get("X-Tenant-Id"))
			assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("raw-binary")), capturedHeaders.Get("X-Token-Bin"))
		})
	}
}

func TestConnectRPCGRPCSubgraphRetriesAndOriginHooks(t *testing.T) {
	var attempts atomic.Int32
	server := newProjectsConnectServer(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == projects.ProjectsService_QueryProject_FullMethodName && attempts.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"code":"unavailable","message":"retry once"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	hooks := &connectOriginHooksModule{}
	testenv.Run(t, connectRPCSubgraphTestConfig(server.URL+"/rpc/", "proto",
		core.WithSubgraphRetryOptions(true, "", 1, time.Second, time.Millisecond, "true", nil),
		core.WithCustomModules(hooks),
	), func(t *testing.T, xEnv *testenv.Environment) {
		response := xEnv.MakeGraphQLRequestOK(testenv.GraphQLRequest{
			Query: `query { project(id: 1) { id name } }`,
		})
		assert.Equal(t, `{"data":{"project":{"id":"1","name":"Cloud Migration Overhaul"}}}`, response.Body)
	})

	assert.Equal(t, int32(2), attempts.Load())
	assert.Equal(t, int32(1), hooks.requestCalls.Load())
	assert.Equal(t, int32(1), hooks.responseCalls.Load())
	assert.Equal(t, "projects", hooks.subgraphName())
	assert.NotEmpty(t, hooks.requestBody())
	assert.NotEmpty(t, hooks.responseBody())
}

func TestConnectRPCGRPCSubgraphMutationIsNotRetried(t *testing.T) {
	var attempts atomic.Int32
	server := newProjectsConnectServer(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == projects.ProjectsService_MutationUpdateProjectStatus_FullMethodName {
				attempts.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"code":"unavailable","message":"do not retry"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	testenv.Run(t, connectRPCSubgraphTestConfig(server.URL+"/rpc/", "proto",
		core.WithSubgraphRetryOptions(true, "", 3, time.Second, time.Millisecond, "true", nil),
	), func(t *testing.T, xEnv *testenv.Environment) {
		response, err := xEnv.MakeGraphQLRequest(testenv.GraphQLRequest{
			Query: `mutation { updateProjectStatus(projectId: "1", status: COMPLETED) { id } }`,
		})
		require.NoError(t, err)
		assert.Contains(t, response.Body, `"errors"`)
	})

	assert.Equal(t, int32(1), attempts.Load())
}

func TestConnectRPCGRPCSubgraphEncodingMismatchDoesNotFallback(t *testing.T) {
	var (
		attempts    atomic.Int32
		contentType atomic.Value
	)
	server := newProjectsConnectServer(t, func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			contentType.Store(r.Header.Get("Content-Type"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnsupportedMediaType)
			_, _ = w.Write([]byte(`{"code":"unimplemented","message":"json encoding is disabled"}`))
		})
	})

	testenv.Run(t, connectRPCSubgraphTestConfig(server.URL+"/rpc/", "json",
		core.WithSubgraphRetryOptions(false, "", 0, 0, 0, "", nil),
	), func(t *testing.T, xEnv *testenv.Environment) {
		response, err := xEnv.MakeGraphQLRequest(testenv.GraphQLRequest{
			Query: `query { project(id: 1) { id } }`,
		})
		require.NoError(t, err)
		assert.Contains(t, response.Body, `"errors"`)
	})

	require.Equal(t, int32(1), attempts.Load())
	assert.Equal(t, "application/json", contentType.Load())
}

func TestConnectRPCGRPCSubgraphRequestTimeout(t *testing.T) {
	server := newProjectsConnectServer(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == projects.ProjectsService_QueryProject_FullMethodName {
				select {
				case <-time.After(250 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})

	trafficRules := config.TrafficShapingRules{
		All: config.GlobalSubgraphRequestRule{
			RequestTimeout: testutils.ToPtr(50 * time.Millisecond),
		},
	}

	testenv.Run(t, connectRPCSubgraphTestConfig(server.URL+"/rpc/", "proto",
		core.WithSubgraphRetryOptions(false, "", 0, 0, 0, "", nil),
		core.WithSubgraphTransportOptions(core.NewSubgraphTransportOptions(trafficRules)),
	), func(t *testing.T, xEnv *testenv.Environment) {
		started := time.Now()
		response, err := xEnv.MakeGraphQLRequest(testenv.GraphQLRequest{
			Query: `query { project(id: 1) { id name } }`,
		})
		require.NoError(t, err)
		assert.Contains(t, response.Body, `"errors"`)
		assert.Less(t, time.Since(started), 200*time.Millisecond)
	})
}

func newProjectsConnectServer(t *testing.T, middleware func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()

	projectsHandler, err := projectsservice.NewConnectHandler(&projectsservice.ProjectsService{})
	require.NoError(t, err)

	mutationHandler := connect.NewUnaryHandler(
		projects.ProjectsService_MutationUpdateProjectStatus_FullMethodName,
		func(_ context.Context, request *connect.Request[projects.MutationUpdateProjectStatusRequest]) (*connect.Response[projects.MutationUpdateProjectStatusResponse], error) {
			return connect.NewResponse(&projects.MutationUpdateProjectStatusResponse{
				UpdateProjectStatus: &projects.ProjectUpdate{
					Id:          "connect-update",
					ProjectId:   request.Msg.ProjectId,
					UpdateType:  projects.ProjectUpdateType_PROJECT_UPDATE_TYPE_STATUS_CHANGE,
					Description: "Updated without shared fixture state",
				},
			}), nil
		},
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == projects.ProjectsService_MutationUpdateProjectStatus_FullMethodName {
			mutationHandler.ServeHTTP(w, r)
			return
		}
		projectsHandler.ServeHTTP(w, r)
	})
	var root http.Handler = handler
	if middleware != nil {
		root = middleware(root)
	}

	mux := http.NewServeMux()
	mux.Handle("/rpc/", http.StripPrefix("/rpc", root))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func connectRPCSubgraphTestConfig(baseURL, encoding string, options ...core.Option) *testenv.Config {
	options = append([]core.Option{
		core.WithGRPCProtocol(config.GRPCProtocolConfiguration{
			DefaultProtocol:    "connectrpc",
			ConnectRPCEncoding: encoding,
		}),
	}, options...)

	return &testenv.Config{
		RouterConfigJSONTemplate: testenv.ConfigWithGRPCJSONTemplate,
		NoRetryClient:            true,
		RouterOptions:            options,
		ModifyRouterConfig: func(routerConfig *nodev1.RouterConfig) {
			for _, subgraph := range routerConfig.Subgraphs {
				if subgraph.Name == "projects" {
					subgraph.RoutingUrl = baseURL
				}
			}
			for _, datasource := range routerConfig.GetEngineConfig().GetDatasourceConfigurations() {
				graphql := datasource.GetCustomGraphql()
				if graphql.GetGrpc() != nil {
					graphql.GetFetch().GetUrl().StaticVariableContent = baseURL
				}
			}
		},
	}
}

type connectOriginHooksModule struct {
	requestCalls  atomic.Int32
	responseCalls atomic.Int32

	mu              sync.Mutex
	activeSubgraph  string
	requestPayload  []byte
	responsePayload []byte
}

func (m *connectOriginHooksModule) Module() core.ModuleInfo {
	return core.ModuleInfo{
		ID: "connect-origin-hooks-test",
		New: func() core.Module {
			return m
		},
	}
}

func (m *connectOriginHooksModule) OnOriginRequest(request *http.Request, requestContext core.RequestContext) (*http.Request, *http.Response) {
	m.requestCalls.Add(1)
	payload, _ := io.ReadAll(request.Body)
	request.Body = io.NopCloser(bytes.NewReader(payload))

	m.mu.Lock()
	m.requestPayload = append([]byte(nil), payload...)
	if subgraph := requestContext.ActiveSubgraph(request); subgraph != nil {
		m.activeSubgraph = subgraph.Name
	}
	m.mu.Unlock()
	return request, nil
}

func (m *connectOriginHooksModule) OnOriginResponse(response *http.Response, _ core.RequestContext) *http.Response {
	m.responseCalls.Add(1)
	if response == nil || response.Body == nil {
		return response
	}
	payload, _ := io.ReadAll(response.Body)
	response.Body = io.NopCloser(bytes.NewReader(payload))

	m.mu.Lock()
	m.responsePayload = append([]byte(nil), payload...)
	m.mu.Unlock()
	return response
}

func (m *connectOriginHooksModule) subgraphName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeSubgraph
}

func (m *connectOriginHooksModule) requestBody() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.requestPayload...)
}

func (m *connectOriginHooksModule) responseBody() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.responsePayload...)
}

var (
	_ core.Module                  = (*connectOriginHooksModule)(nil)
	_ core.EnginePreOriginHandler  = (*connectOriginHooksModule)(nil)
	_ core.EnginePostOriginHandler = (*connectOriginHooksModule)(nil)
)
