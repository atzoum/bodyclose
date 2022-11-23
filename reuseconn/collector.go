package reuseconn

import (
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

type Fn struct {
	Pkg, Name string
}
type ResponseDisposers struct {
	Fns []Fn
}

func (*ResponseDisposers) AFact() {
	// just a marker method
}

func (r *ResponseDisposers) String() string {
	var fns []string
	for _, f := range r.Fns {
		fns = append(fns, f.Name)
	}
	return "responseDisposers:" + strings.Join(fns, ",")
}

type readCloserFuncCollector struct {
	facts map[string]*ResponseDisposers
}

// analyze collects all functions that read the response body to completion and close it.
func (c *readCloserFuncCollector) analyze(pass *analysis.Pass) {
	var disposerFns []*ssa.Function
	funcs := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).SrcFuncs
	for _, f := range funcs {
		respOrRespBodyParam := func() *ssa.Parameter {
			if f.Signature.Params().Len() == 1 {
				paramTypeString := f.Signature.Params().At(0).Type().String()
				if paramTypeString == readCloserType || paramTypeString == responseType {
					return f.Params[0]
				}
			}
			return nil
		}()
		if respOrRespBodyParam == nil {
			continue
		}
		if len(*respOrRespBodyParam.Referrers()) == 0 {
			continue
		}

		var read, closed bool

		switch respOrRespBodyParam.Type().String() {
		case readCloserType: // the body was passed as a function parameter
			for _, ref := range *respOrRespBodyParam.Referrers() {
				if !read && isReadCall(ref) {
					read = true
				}
				if !closed && isCloseCall(ref) {
					closed = true
				}
			}
		case responseType: // the whole response was passed as a function parameter
			for _, ref := range *respOrRespBodyParam.Referrers() {
				if field, okField := ref.(*ssa.FieldAddr); okField { // referrer must get the body field
					if p, okPointer := field.Type().(*types.Pointer); okPointer && isNamedType(p.Elem(), "io", "ReadCloser") {
						for _, ref := range *field.Referrers() { // body must be read or closed
							if isCloseCall(ref) {
								closed = true
							}
							if isReadCall(ref) {
								read = true
							}
						}
					}
				}
			}
		}
		if read && closed {
			disposerFns = append(disposerFns, f)
		}
	}
	if len(disposerFns) > 0 {
		var disposerFnsNames []Fn
		for _, f := range disposerFns {
			disposerFnsNames = append(disposerFnsNames, Fn{Pkg: f.Pkg.Pkg.Path(), Name: f.Name()})
		}
		pass.ExportPackageFact(&ResponseDisposers{Fns: disposerFnsNames})
	}
}

// isDisposeCall returns true if the given instruction is a call to a function that reads and closes the response body.
func (c *readCloserFuncCollector) isDisposeCall(pass *analysis.Pass, call ssa.Instruction) bool {
	if call, ok := call.(*ssa.Call); ok { // needs to be a function call
		if f, ok := call.Common().Value.(*ssa.Function); ok {
			disposerFns := c.getDisposers(pass, f.Package().Pkg)
			for _, disposer := range disposerFns {
				if f.Name() == disposer.Name &&
					f.Package().Pkg.Path() == disposer.Pkg {
					return true
				}
			}
		}
	}
	if call, ok := call.(*ssa.Defer); ok { // needs to be a function call
		if f, ok := call.Common().Value.(*ssa.Function); ok {
			disposerFns := c.getDisposers(pass, f.Package().Pkg)
			for _, disposer := range disposerFns {
				if f.Name() == disposer.Name &&
					f.Package().Pkg.Path() == disposer.Pkg {
					return true
				}
			}
		}
	}
	return false
}

func (c *readCloserFuncCollector) getDisposers(pass *analysis.Pass, pkg *types.Package) []Fn {
	if c.facts == nil {
		c.facts = make(map[string]*ResponseDisposers)
	}
	if f, ok := c.facts[pkg.Path()]; ok {
		return f.Fns
	}
	var disposerFns ResponseDisposers
	if pass.ImportPackageFact(pkg, &disposerFns) {
		c.facts[pkg.Path()] = &disposerFns
	}
	return disposerFns.Fns
}

// isCloseCall returns true if the given instruction is a call to a function that closes the response body.
func isCloseCall(ccall ssa.Instruction) bool {
	return isMethodCall(ccall, closeMethod, "io.Closer")
}

// isReadCall returns true if the given instruction is a call to a function that reads the response body.
func isReadCall(ccall ssa.Instruction) bool {
	return isMethodCall(ccall, readMethod, "io.Reader")
}

func isMethodCall(ccall ssa.Instruction, methodName, interfaceName string) bool {
	switch ccall := ccall.(type) {
	case *ssa.UnOp: // pointer indirection
		if ccall.Op == token.MUL {
			for _, ref := range *ccall.Referrers() {
				if isMethodCall(ref, methodName, interfaceName) {
					return true
				}
			}
		}
	case *ssa.Defer: // defer call
		if ccall.Call.Method != nil && ccall.Call.Method.Name() == methodName {
			return true
		}
	case *ssa.Call: // method call
		if ccall.Call.Method != nil && ccall.Call.Method.Name() == methodName {
			return true
		}
	case *ssa.ChangeInterface:
		if ccall.Type().String() == interfaceName {
			mtd := ccall.Type().Underlying().(*types.Interface).Method(0)
			crs := *ccall.Referrers()
			for _, cs := range crs {
				if cs, ok := cs.(*ssa.Defer); ok {
					if val, ok := cs.Common().Value.(*ssa.Function); ok {
						for _, b := range val.Blocks {
							for _, instr := range b.Instrs {
								if c, ok := instr.(*ssa.Call); ok {
									if c.Call.Method == mtd {
										return true
									}
								}
							}
						}
					}
				}

				if returnOp, ok := cs.(*ssa.Return); ok {
					for _, resultValue := range returnOp.Results {
						if resultValue.Type().String() == interfaceName {
							return true
						}
					}
				}

				if cs, ok := cs.(*ssa.Call); ok {
					if cs.Call.Method == mtd {
						return true
					}
					for _, arg := range cs.Call.Args {
						if arg.Type().String() == interfaceName {
							return true
						}
					}
				}
			}
		}
	case *ssa.Return:
		for _, resultValue := range ccall.Results {
			if resultValue.Type().String() == "io.ReadCloser" || resultValue.Type().String() == interfaceName {
				return true
			}
		}
	}
	return false
}
