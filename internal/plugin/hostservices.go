package plugin

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/jenken827/pfm-plugin-sdk/proto/gen/plugin"
	"github.com/jenken827/pfm-proto/proto/gen/native"
	"github.com/jenken827/pfm/internal/db"
	"github.com/jenken827/pfm/pkg/llog"
)

// hostServer 插件反向调用宿主的服务（回写配置、更新缓存状态、写日志、读文件）
//
// 监听 127.0.0.1 随机端口，token 与插件侧共享；插件进程与宿主同用户，
// 这个 token 只是防"同机其它进程顺手误调"，不是安全边界。
type hostServer struct {
	addr   string
	token  string
	server *http.Server
}

// startHostServer 启动反向服务
func startHostServer() (*hostServer, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("宿主服务监听失败: %w", err)
	}
	host := &hostServer{addr: listener.Addr().String(), token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/UpdateProviderConfig", host.auth(host.handleUpdateProviderConfig))
	mux.HandleFunc("/rpc/UpdateCacheStatus", host.auth(host.handleUpdateCacheStatus))
	mux.HandleFunc("/rpc/Log", host.auth(host.handleLog))
	mux.HandleFunc("/rpc/ReadFile", host.auth(host.handleReadFile))
	host.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if serveErr := host.server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			llog.L().Warnf("宿主服务退出: %v", serveErr)
		}
	}()
	return host, nil
}

// Close 关闭反向服务
func (h *hostServer) Close() {
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = h.server.Shutdown(ctx)
	}
}

func (h *hostServer) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+h.token {
			writeHostError(w, http.StatusUnauthorized, "token 不匹配")
			return
		}
		next(w, r)
	}
}

func (h *hostServer) handleUpdateProviderConfig(w http.ResponseWriter, r *http.Request) {
	req := &plugin.UpdateProviderConfigReq{}
	if !decodeHostRequest(w, r, req) {
		return
	}
	if err := db.UpdateStorageProviderConfig(req.GetProviderId(), req.GetConfigJson()); err != nil {
		writeHostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	llog.L().Infof("plugin 回写 provider %d 配置", req.GetProviderId())
	writeHostProto(w, &plugin.Empty{})
}

func (h *hostServer) handleUpdateCacheStatus(w http.ResponseWriter, r *http.Request) {
	req := &plugin.UpdateCacheStatusReq{}
	if !decodeHostRequest(w, r, req) {
		return
	}
	if err := db.UpdateFileLocationCacheStatus(req.GetLocationId(), native.CacheStatus(req.GetStatus())); err != nil {
		writeHostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHostProto(w, &plugin.Empty{})
}

func (h *hostServer) handleLog(w http.ResponseWriter, r *http.Request) {
	req := &plugin.LogReq{}
	if !decodeHostRequest(w, r, req) {
		return
	}
	switch req.GetLevel() {
	case "debug":
		llog.L().Debugf("[plugin] %s", req.GetMessage())
	case "warn":
		llog.L().Warnf("[plugin] %s", req.GetMessage())
	case "error":
		llog.L().Errorf("[plugin] %s", req.GetMessage())
	default:
		llog.L().Infof("[plugin] %s", req.GetMessage())
	}
	writeHostProto(w, &plugin.Empty{})
}

func decodeHostRequest(w http.ResponseWriter, r *http.Request, req proto.Message) bool {
	if r.Method != http.MethodPost {
		writeHostError(w, http.StatusMethodNotAllowed, "只接受 POST")
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeHostError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if err := protojson.Unmarshal(raw, req); err != nil {
		writeHostError(w, http.StatusBadRequest, fmt.Sprintf("解析请求失败: %v", err))
		return false
	}
	return true
}

func writeHostError(w http.ResponseWriter, status int, message string) {
	raw, _ := protojson.Marshal(&plugin.Error{Code: proto.String("HOST"), Message: proto.String(message)})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func writeHostProto(w http.ResponseWriter, msg proto.Message) {
	body, err := protojson.Marshal(msg)
	if err != nil {
		writeHostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
