package processor

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/WindowsSov8forUs/glyccat/database"
	"github.com/WindowsSov8forUs/glyccat/log"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/paginated"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

// Adapter 在 QQ 适配器上补充应用缓存，不重新实现平台通信
// 服务端只注册本封装一次，原生路由和可选生命周期接口由内层适配器保留。
type Adapter struct {
	*qq.Adapter
	store     *database.MessageStore
	appID     string
	routes    map[string]server.RouteCall[any, any]
	events    chan *event.Event
	eventOnce sync.Once
	closed    context.Context
	cancel    context.CancelFunc
}

func NewAdapter(inner *qq.Adapter, store *database.MessageStore, appID string) (*Adapter, error) {
	if inner == nil || appID == "" {
		return nil, fmt.Errorf("QQ 适配器与账号信息不能为空")
	}
	closed, cancel := context.WithCancel(context.Background())
	a := &Adapter{
		Adapter: inner, store: store, appID: appID,
		routes: make(map[string]server.RouteCall[any, any]),
		events: make(chan *event.Event), closed: closed, cancel: cancel,
	}
	for name, handler := range inner.Routes() {
		a.routes[name] = handler
	}
	a.registerCacheRoutes()
	return a, nil
}

func (a *Adapter) Routes() map[string]server.RouteCall[any, any] {
	return a.routes
}

func (a *Adapter) GetLogins(ctx context.Context) ([]*login.Login, error) {
	logins, err := a.Adapter.GetLogins(ctx)
	if err != nil {
		return nil, err
	}
	for i, value := range logins {
		logins[i] = a.withCacheFeatures(value)
	}
	return logins, nil
}

func (a *Adapter) withCacheFeatures(value *login.Login) *login.Login {
	if value == nil {
		return nil
	}
	result := value.Clone()
	if a.store == nil || result.Platform != "qq" {
		return result
	}
	for _, feature := range []string{"message.get", "message.list", "message.list.from", "channel.get", "channel.list", "guild.list"} {
		found := false
		for _, existing := range result.Features {
			if existing == feature {
				found = true
				break
			}
		}
		if !found {
			result.Features = append(result.Features, feature)
		}
	}
	return result
}

// Publisher 只有此处读取内层事件通道，缓存完成一次处理后继续交付原事件
func (a *Adapter) Publisher(ctx context.Context) <-chan *event.Event {
	a.eventOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(a.closed, cancel)
		go func() {
			defer close(a.events)
			defer cancel()
			defer stop()
			source := a.Adapter.Publisher(runCtx)
			for {
				select {
				case <-runCtx.Done():
					return
				case evt, ok := <-source:
					if !ok {
						return
					}
					if evt == nil {
						continue
					}
					if err := a.cacheEvent(evt); err != nil {
						log.Errorf("缓存接收事件失败: %v", err)
					}
					copied := *evt
					copied.Login = a.withCacheFeatures(evt.Login)
					select {
					case a.events <- &copied:
					case <-runCtx.Done():
						return
					}
				}
			}
		}()
	})
	return a.events
}

func (a *Adapter) Cleanup(ctx context.Context) error {
	a.cancel()
	return a.Adapter.Cleanup(ctx)
}

func (a *Adapter) scope(platform, selfID, channelID string) database.MessageScope {
	return database.MessageScope{AppID: a.appID, Platform: platform, SelfID: selfID, ChannelID: channelID}
}

func (a *Adapter) registerCacheRoutes() {
	a.localRoute("message.get", server.Wrapper(func(request *server.Request[server.MessageOpParam]) (any, error) {
		msg, err := a.store.Get(a.scope(request.Platform, request.SelfID, request.Params.ChannelID), request.Params.MessageID)
		return msg, cacheError(err)
	}))
	a.localRoute("message.list", server.Wrapper(func(request *server.Request[server.MessageListParam]) (any, error) {
		params := request.Params
		result, err := a.store.List(requestContext(request.Origin), a.scope(request.Platform, request.SelfID, params.ChannelID),
			params.Next.ValueOr(""), string(params.Direction.ValueOr("before")), string(params.Order.ValueOr("asc")), params.Limit.ValueOr(0))
		return result, cacheError(err)
	}))
	a.localRoute("channel.get", server.Wrapper(func(request *server.Request[server.ChannelParam]) (any, error) {
		item, err := a.store.GetConversation(a.scope(request.Platform, request.SelfID, request.Params.ChannelID))
		if err != nil {
			return nil, cacheError(err)
		}
		return item.Channel, nil
	}))
	a.localRoute("channel.list", server.Wrapper(func(request *server.Request[server.ChannelListParam]) (any, error) {
		if request.Params.Next.ValueOr("") != "" {
			return nil, server.BadRequest("QQ 已观察群组只有一个消息频道，不支持后续分页")
		}
		item, err := a.store.GetConversation(a.scope(request.Platform, request.SelfID, request.Params.GuildID))
		if err != nil {
			return nil, cacheError(err)
		}
		if item.Channel.Type == channel.ChannelTypeDirect {
			return nil, server.NotFound("私聊不属于群组频道列表")
		}
		return paginated.Paginated[channel.Channel]{Data: []channel.Channel{*item.Channel}}, nil
	}))
	a.localRoute("guild.list", server.Wrapper(func(request *server.Request[server.GuildListParam]) (any, error) {
		result, err := a.store.ListGuilds(requestContext(request.Origin), a.scope(request.Platform, request.SelfID, ""), request.Params.Next.ValueOr(""))
		return result, cacheError(err)
	}))

	loginGet := a.routes["login.get"]
	a.routes["login.get"] = func(request *server.Request[any]) (any, error) {
		result, err := loginGet(request)
		if err == nil {
			if value, ok := result.(*login.Login); ok {
				return a.withCacheFeatures(value), nil
			}
		}
		return result, err
	}
	create := a.routes["message.create"]
	a.routes["message.create"] = server.Wrapper(func(request *server.Request[server.MessageCreateParam]) (any, error) {
		result, err := create(forwardRequest(request))
		if err == nil && a.store != nil && request.Platform == "qq" {
			if cacheErr := a.cacheSent(request, result); cacheErr != nil {
				// QQ 已经完成的发送不能因为缓存失败而被当作未发送。
				log.Errorf("缓存已发送消息失败: %v", cacheErr)
			}
		}
		return result, err
	})
	remove := a.routes["message.delete"]
	a.routes["message.delete"] = server.Wrapper(func(request *server.Request[server.MessageOpParam]) (any, error) {
		result, err := remove(forwardRequest(request))
		if err == nil && responseSucceeded(result) && a.store != nil && request.Platform == "qq" {
			cacheErr := a.store.Delete(a.scope(request.Platform, request.SelfID, request.Params.ChannelID), request.Params.MessageID)
			if cacheErr != nil && !errors.Is(cacheErr, database.ErrNotFound) {
				log.Errorf("删除已撤回消息缓存失败: %v", cacheErr)
			}
		}
		return result, err
	})
}

// localRoute 仅增强 QQ 本地缓存动作；频道平台及关闭缓存时仍委托 SDK
func (a *Adapter) localRoute(name string, handler server.RouteCall[any, any]) {
	original := a.routes[name]
	a.routes[name] = func(request *server.Request[any]) (any, error) {
		if request.Platform == "qq" && a.store != nil {
			return handler(request)
		}
		if original == nil {
			return nil, server.NotFound("当前平台不支持此接口")
		}
		return original(request)
	}
}

func forwardRequest[T any](request *server.Request[T]) *server.Request[any] {
	return &server.Request[any]{Origin: request.Origin, Action: request.Action, Params: request.Params, Platform: request.Platform, SelfID: request.SelfID}
}

func cacheError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		return server.NotFound("本机缓存中没有对应记录")
	case errors.Is(err, database.ErrInvalid):
		return server.BadRequest(err.Error())
	case errors.Is(err, database.ErrStoreClosed):
		return server.NewActionError(503, "消息缓存暂不可用", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		log.Errorf("读取消息缓存失败: %v", err)
		return server.NewActionError(500, "读取本机缓存失败", err)
	}
}

func responseSucceeded(result any) bool {
	switch value := result.(type) {
	case *server.Response:
		return value != nil && (value.StatusCode == 0 || (value.StatusCode >= 200 && value.StatusCode < 300))
	case server.Response:
		return value.StatusCode == 0 || (value.StatusCode >= 200 && value.StatusCode < 300)
	default:
		return true
	}
}

var _ server.Adapter = (*Adapter)(nil)
var _ server.EventPublisher = (*Adapter)(nil)
var _ server.RootRouteRegistrar = (*Adapter)(nil)
var _ server.Preparable = (*Adapter)(nil)
var _ server.Blockable = (*Adapter)(nil)
var _ server.Cleanable = (*Adapter)(nil)
