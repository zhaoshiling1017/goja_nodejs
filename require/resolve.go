package require

import (
	"encoding/json"
	"path/filepath"
	"strings"

	js "github.com/dop251/goja"
)

// NodeJS module search algorithm described by
// https://nodejs.org/api/modules.html#modules_all_together
func (r *RequireModule) resolve(path string) (module *js.Object, err error) {
	origPath, path := path, filepathClean(path)
	if path == "" {
		return nil, IllegalModuleNameError
	}

	module, err = r.loadNative(path)
	if err == nil {
		return
	}

	var start string
	err = nil
	if strings.HasPrefix(origPath, "/") {
		start = "/"
	} else {
		start = r.getCurrentModulePath()
	}

	p := filepath.Join(start, path)
	if strings.HasPrefix(origPath, "./") ||
		strings.HasPrefix(origPath, "/") || strings.HasPrefix(origPath, "../") ||
		origPath == "." || origPath == ".." {
		if module = r.modules[p]; module != nil {
			return
		}
		module, err = r.loadAsFileOrDirectory(p)
		if err == nil {
			r.modules[p] = module
		}
	} else {
		if module = r.nodeModules[p]; module != nil {
			return
		}
		module, err = r.loadNodeModules(path, start)
		if err == nil {
			r.nodeModules[p] = module
		}
	}

	return
}

func (r *RequireModule) loadNative(path string) (*js.Object, error) {
	module := r.modules[path]
	if module != nil {
		return module, nil
	}

	var ldr ModuleLoader
	if ldr = r.r.native[path]; ldr == nil {
		ldr = native[path]
	}

	if ldr != nil {
		module = r.createModuleObject()
		r.modules[path] = module
		ldr(r.runtime, module)
		return module, nil
	}

	return nil, InvalidModuleError
}

func (r *RequireModule) loadAsFileOrDirectory(path string) (module *js.Object, err error) {
	module, err = r.loadAsFile(path)
	if err == nil {
		return
	}

	return r.loadAsDirectory(path)
}

func (r *RequireModule) loadAsFile(path string) (module *js.Object, err error) {
	if module, err = r.loadModule(path); err == nil {
		return
	}

	p := path + ".js"
	if module, err = r.loadModule(p); err == nil {
		return
	}

	p = path + ".json"
	return r.loadModule(p)
}

func (r *RequireModule) loadIndex(path string) (module *js.Object, err error) {
	p := filepath.Join(path, "index.js")
	if module, err = r.loadModule(p); err == nil {
		return
	}

	p = filepath.Join(path, "index.json")
	return r.loadModule(p)
}

func (r *RequireModule) loadAsDirectory(path string) (module *js.Object, err error) {
	p := filepath.Join(path, "package.json")
	buf, err := r.r.getSource(p)
	if err != nil {
		return r.loadIndex(path)
	}
	var pkg struct {
		Main string
	}
	err = json.Unmarshal(buf, &pkg)
	if err != nil || len(pkg.Main) == 0 {
		return r.loadIndex(path)
	}

	m := filepath.Join(path, pkg.Main)
	if module, err = r.loadAsFile(m); err == nil {
		return
	}

	return r.loadIndex(m)
}

func (r *RequireModule) loadNodeModule(path, start string) (*js.Object, error) {
	return r.loadAsFileOrDirectory(filepath.Join(start, path))
}

func (r *RequireModule) loadNodeModules(path, start string) (module *js.Object, err error) {
	for _, dir := range r.r.globalFolders {
		if module, err = r.loadNodeModule(path, dir); err == nil {
			return
		}
	}
	for {
		var p string
		if filepath.Base(start) != "node_modules" {
			p = filepath.Join(start, "node_modules")
		} else {
			p = start
		}
		if module, err = r.loadNodeModule(path, p); err == nil {
			return
		}
		if start == ".." { // Dir('..') is '.'
			break
		}
		parent := filepath.Dir(start)
		if parent == start {
			break
		}
		start = parent
	}

	return nil, InvalidModuleError
}

func (r *RequireModule) getCurrentModulePath() string {
	var buf [2]js.StackFrame
	frames := r.runtime.CaptureCallStack(2, buf[:0])
	if len(frames) < 2 {
		return "."
	}
	return filepath.Dir(frames[1].SrcName())
}

func (r *RequireModule) createModuleObject() *js.Object {
	module := r.runtime.NewObject()
	module.Set("exports", r.runtime.NewObject())
	return module
}

func (r *RequireModule) loadModule(path string) (*js.Object, error) {
	module := r.modules[path]
	if module == nil {
		module = r.createModuleObject()
		r.modules[path] = module
		err := r.loadModuleFile(path, module)
		if err != nil {
			module = nil
			delete(r.modules, path)
		}
		return module, err
	}
	return module, nil
}

func (r *RequireModule) loadModuleFile(path string, jsModule *js.Object) error {

	prg, err := r.r.getCompiledSource(path)

	if err != nil {
		return err
	}

	f, err := r.runtime.RunProgram(prg)
	if err != nil {
		return err
	}

	if call, ok := js.AssertFunction(f); ok {
		jsExports := jsModule.Get("exports")
		jsRequire := r.runtime.Get("require")

		// Run the module source, with "jsExports" as "this",
		// "jsExports" as the "exports" variable, "jsRequire"
		// as the "require" variable and "jsModule" as the
		// "module" variable (Nodejs capable).
		_, err = call(jsExports, jsExports, jsRequire, jsModule)
		if err != nil {
			return err
		}
	} else {
		return InvalidModuleError
	}

	return nil
}
