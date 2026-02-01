//go:build !no_jsparser

package js

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/dop251/goja"
	"github.com/merisssas/bot/config"
	"github.com/merisssas/bot/pkg/parser"
)

type jsParser struct {
	meta  PluginMeta
	vm    *goja.Runtime
	reqCh chan jsParserReq
}

type jsParserReq struct {
	method ParserMethod
	ctx    context.Context
	url    string
	respCh chan jsParserResp
}

type jsParserResp struct {
	item *parser.Item
	ok   bool
	err  error
}

func (p *jsParser) CanHandle(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), pluginTimeout())
	defer cancel()
	respCh := make(chan jsParserResp, 1)
	if err := p.sendReq(ctx, jsParserReq{method: ParserMethodCanHandle, ctx: ctx, url: url, respCh: respCh}); err != nil {
		return false
	}
	select {
	case resp := <-respCh:
		return resp.ok && resp.err == nil
	case <-ctx.Done():
		return false
	}
}

func (p *jsParser) Parse(ctx context.Context, url string) (*parser.Item, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	respCh := make(chan jsParserResp, 1)
	if err := p.sendReq(ctx, jsParserReq{method: ParserMethodParse, ctx: ctx, url: url, respCh: respCh}); err != nil {
		return nil, err
	}
	select {
	case resp := <-respCh:
		return resp.item, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *jsParser) sendReq(ctx context.Context, req jsParserReq) error {
	select {
	case p.reqCh <- req:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newJSParser(vm *goja.Runtime, canHandleFunc, parseFunc goja.Value, metadata PluginMeta) *jsParser {
	p := &jsParser{
		vm:    vm,
		reqCh: make(chan jsParserReq, 10),
		meta:  metadata,
	}

	go func() {
		for req := range p.reqCh {
			switch req.method {
			case ParserMethodCanHandle:
				fn, _ := goja.AssertFunction(canHandleFunc)
				res, err := runWithTimeout(req.ctx, p.vm, pluginTimeout(), func() (goja.Value, error) {
					return fn(goja.Undefined(), p.vm.ToValue(req.url))
				})
				if err != nil {
					req.respCh <- jsParserResp{ok: false, err: err}
					continue
				}
				req.respCh <- jsParserResp{ok: res.ToBoolean()}
			case ParserMethodParse:
				fn, _ := goja.AssertFunction(parseFunc)
				result, err := runWithTimeout(req.ctx, p.vm, pluginTimeout(), func() (goja.Value, error) {
					return fn(goja.Undefined(), p.vm.ToValue(req.url))
				})
				if err != nil {
					req.respCh <- jsParserResp{err: err}
					continue
				}

				var item parser.Item
				if exported := result.Export(); exported != nil {
					data, err := json.Marshal(exported)
					if err != nil {
						req.respCh <- jsParserResp{err: fmt.Errorf("failed to marshal result to JSON: %w", err)}
						continue
					}

					if err := json.Unmarshal(data, &item); err != nil {
						req.respCh <- jsParserResp{err: fmt.Errorf("failed to unmarshal JSON to Item: %w", err)}
						continue
					}
				} else {
					req.respCh <- jsParserResp{err: fmt.Errorf("JS function returned null or undefined")}
					continue
				}
				req.respCh <- jsParserResp{item: &item}
			}
		}
	}()

	return p
}

// 加载指定文件夹下的所有 JS 解析器插件
func LoadPlugins(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".js" {
			continue
		}
		scriptPath := filepath.Join(dir, e.Name())
		code, err := os.ReadFile(scriptPath)
		if err != nil {
			return err
		}

		vm := goja.New()
		vm.Set("registerParser", jsRegisterParser(vm))
		// Inject some utils to vm
		logger := log.FromContext(ctx).WithPrefix(fmt.Sprintf("[plugin|parser]/%s", e.Name()))
		vm.Set("console", jsConsole(logger))
		// http fetch funcs
		vm.Set("ghttp", jsGhttp(vm))
		// playwright fetch func
		vm.Set("playwright", jsPlaywright(vm, logger))

		if _, err := runWithTimeout(ctx, vm, pluginTimeout(), func() (goja.Value, error) {
			return vm.RunString(string(code))
		}); err != nil {
			return fmt.Errorf("error loading plugin %s: %w", e.Name(), err)
		}
	}
	return nil
}

var (
	pluginNameMu sync.Map
)

func AddPlugin(ctx context.Context, code string, name string) error {
	pluginName, err := sanitizePluginFilename(name)
	if err != nil {
		return err
	}

	value, _ := pluginNameMu.LoadOrStore(pluginName, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	return addPlugin(ctx, code, pluginName)
}

func addPlugin(ctx context.Context, code string, name string) error {
	logger := log.FromContext(ctx).WithPrefix(fmt.Sprintf("[plugin|parser]/%s", name))
	vm := goja.New()
	vm.Set("registerParser", jsRegisterParser(vm))
	vm.Set("console", jsConsole(logger))
	vm.Set("ghttp", jsGhttp(vm))
	vm.Set("playwright", jsPlaywright(vm, logger))
	if _, err := runWithTimeout(ctx, vm, pluginTimeout(), func() (goja.Value, error) {
		return vm.RunString(code)
	}); err != nil {
		return fmt.Errorf("error loading plugin %s: %w", name, err)
	}
	dir := "plugins"
	configuredDirs := config.C().Parser.PluginDirs
	if len(configuredDirs) > 0 {
		dir = configuredDirs[0]
	}
	if err := os.MkdirAll(dir, 0755); err == nil {
		pluginPath := filepath.Join(dir, name)
		if err := os.WriteFile(pluginPath, []byte(code), 0644); err != nil {
			logger.Warn("Failed to save plugin file: " + err.Error())
		}
	}
	return nil
}

func pluginTimeout() time.Duration {
	timeoutSeconds := config.C().Parser.PluginTimeoutSeconds
	if timeoutSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(timeoutSeconds) * time.Second
}

func runWithTimeout(ctx context.Context, vm *goja.Runtime, timeout time.Duration, fn func() (goja.Value, error)) (goja.Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()

	value, err := fn()
	close(done)
	vm.ClearInterrupt()
	if err != nil {
		var interrupted *goja.InterruptedError
		if errors.As(err, &interrupted) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("js execution interrupted: %w", err)
		}
	}
	return value, err
}

func sanitizePluginFilename(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("plugin filename is empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("plugin filename must not contain path separators")
	}
	if filepath.VolumeName(name) != "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("plugin filename must be a relative name")
	}
	if filepath.Base(name) != name {
		return "", fmt.Errorf("plugin filename must not contain path traversal")
	}
	if filepath.Ext(name) != ".js" {
		return "", fmt.Errorf("plugin filename must have .js extension")
	}
	return name, nil
}
