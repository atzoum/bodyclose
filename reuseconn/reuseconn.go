package reuseconn

import (
	"go/ast"
	"go/types"
	"strconv"
	"strings"

	"github.com/gostaticanalysis/analysisutil"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

var Analyzer = &analysis.Analyzer{
	Name: "reuseconn",
	Doc:  Doc,
	Run:  new(runner).run,
	Requires: []*analysis.Analyzer{
		buildssa.Analyzer,
	},
	FactTypes: []analysis.Fact{&ResponseDisposers{}},
}

const (
	Doc            = "reuseconn checks whether HTTP response body is both closed and consumed, so that the underlying TCP connection can be reused"
	nethttpPath    = "net/http"
	closeMethod    = "Close"
	readMethod     = "Read"
	responseType   = "*net/http.Response"
	readCloserType = "io.ReadCloser"
)

type runner struct {
	funcCollector readCloserFuncCollector
}

// run executes an analysis for the pass. The receiver is passed
// by value because this func is called in parallel for different passes.
func (r *runner) run(pass *analysis.Pass) (interface{}, error) {
	r.funcCollector.analyze(pass)

	funcs := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).SrcFuncs

	responseObj := analysisutil.LookupFromImports(pass.Pkg.Imports(), nethttpPath, "Response")
	if responseObj == nil {
		return nil, nil //nolint:nilnil // this is valid
	}

	skipFile := map[*ast.File]bool{}

	for _, fu := range funcs {
		var skip bool // if the function returns a response it shouldn't be expected to consume or close the body
		for i := 0; i < fu.Signature.Results().Len(); i++ {
			if fu.Signature.Results().At(i).Type().String() == responseType {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		for _, block := range fu.Blocks {
			for i := range block.Instrs {
				pos := block.Instrs[i].Pos()
				if call, ok := getReqCall(block.Instrs[i]); ok {
					if isNotDisposed(call, pass, skipFile, &r.funcCollector) {
						pass.Reportf(pos, "response body must be disposed properly in a single function read to completion and closed")
					}
				}

			}
		}
	}

	return nil, nil
}

// isNotDisposed returns true if the response body is not disposed properly
func isNotDisposed(call *ssa.Call, pass *analysis.Pass, skipFile map[*ast.File]bool, funcCollector *readCloserFuncCollector) bool {
	callReferrers := *call.Referrers()
	if len(callReferrers) == 0 {
		return true // if the call is not used at all, then its response is not disposed
	}
	for _, callReferrer := range callReferrers {
		responseVal, ok := getResponseVal(callReferrer)
		if !ok {
			continue
		}

		if len(*responseVal.Referrers()) == 0 {
			return true // if the response is not used at all, then it is not disposed
		}

		responseReferrers := *responseVal.Referrers()
		for _, responseReferrer := range responseReferrers {
			switch responseReferrer := responseReferrer.(type) {
			case *ssa.Store: // Call in Closure function
				if len(*responseReferrer.Addr.Referrers()) == 0 {
					return true
				}

				for _, addrReferrer := range *responseReferrer.Addr.Referrers() {
					if mc, ok := addrReferrer.(*ssa.MakeClosure); ok {
						f := mc.Fn.(*ssa.Function)
						if noImportedNetHTTP(pass, skipFile, f) {
							// skip this
							return false
						}
						called := isClosureCalled(mc)
						return calledInFunc(pass, skipFile, funcCollector, f, called)
					}

				}
			case *ssa.Call, *ssa.Defer: // Indirect function call
				return !funcCollector.isDisposeCall(pass, responseReferrer)
			case *ssa.FieldAddr: // Reference to response entity
				if responseReferrer.Referrers() == nil {
					return true
				}
				bRefs := *responseReferrer.Referrers()
				for _, bRef := range bRefs {
					bOp, ok := getBodyOp(bRef)
					if !ok {
						continue
					}
					if len(*bOp.Referrers()) == 0 {
						return true
					}
					ccalls := *bOp.Referrers()
					for _, ccall := range ccalls {
						if funcCollector.isDisposeCall(pass, ccall) {
							return false
						}
					}
				}
				return true
			}
		}
	}

	return true
}

// getReqCall returns true if it is a call to the http request function
func getReqCall(instr ssa.Instruction) (*ssa.Call, bool) {
	if call, ok := instr.(*ssa.Call); ok {
		if strings.Contains(call.Type().String(), responseType) {
			return call, true
		}
	}
	return nil, false
}

// getResponseVal returns the response if it can be found
func getResponseVal(instr ssa.Instruction) (ssa.Value, bool) {
	switch instr := instr.(type) {
	case *ssa.FieldAddr:
		if instr.X.Type().String() == responseType {
			return instr.X, true
		}
	case ssa.Value:
		if instr.Type().String() == responseType {
			return instr, true
		}
	}
	return nil, false
}

func getBodyOp(instr ssa.Instruction) (*ssa.UnOp, bool) {
	op, ok := instr.(*ssa.UnOp)
	if !ok {
		return nil, false
	}
	if op.Type().String() != readCloserType {
		return nil, false
	}
	return op, true
}

func isClosureCalled(c *ssa.MakeClosure) bool {
	refs := *c.Referrers()
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		switch ref.(type) {
		case *ssa.Call, *ssa.Defer:
			return true
		}
	}
	return false
}

func noImportedNetHTTP(pass *analysis.Pass, skipFile map[*ast.File]bool, f *ssa.Function) (ret bool) {
	obj := f.Object()
	if obj == nil {
		return false
	}

	file := analysisutil.File(pass, obj.Pos())
	if file == nil {
		return false
	}

	if skip, has := skipFile[file]; has {
		return skip
	}
	defer func() {
		skipFile[file] = ret
	}()

	for _, impt := range file.Imports {
		path, err := strconv.Unquote(impt.Path.Value)
		if err != nil {
			continue
		}
		path = analysisutil.RemoveVendor(path)
		if path == nethttpPath {
			return false
		}
	}

	return true
}

func calledInFunc(pass *analysis.Pass, skipFile map[*ast.File]bool, funcCollector *readCloserFuncCollector, f *ssa.Function, called bool) bool {
	for _, b := range f.Blocks {
		for i, instr := range b.Instrs {
			switch instr := instr.(type) {
			case *ssa.UnOp:
				refs := *instr.Referrers()
				if len(refs) == 0 {
					return true
				}
				for _, r := range refs {
					if v, ok := r.(ssa.Value); ok {
						if ptr, ok := v.Type().(*types.Pointer); !ok || !isNamedType(ptr.Elem(), "io", "ReadCloser") {
							continue
						}
						vrefs := *v.Referrers()
						for _, vref := range vrefs {
							if vref, ok := vref.(*ssa.UnOp); ok {
								vrefs := *vref.Referrers()
								if len(vrefs) == 0 {
									return true
								}
								for _, vref := range vrefs {
									if c, ok := vref.(*ssa.Call); ok {
										if c.Call.Method != nil && c.Call.Method.Name() == closeMethod {
											return !called
										}
									}
								}
							}
						}
					}

				}
			default:
				if call, ok := getReqCall(b.Instrs[i]); ok {
					return isNotDisposed(call, pass, skipFile, funcCollector) || !called
				}
			}
		}
	}
	return false
}

// isNamedType reports whether t is the named type path.name.
func isNamedType(t types.Type, path, name string) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Name() == name && obj.Pkg() != nil && obj.Pkg().Path() == path
}
