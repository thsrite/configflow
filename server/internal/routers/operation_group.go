package routers

import (
 "context"
 "slices"
 "strings"
 "sync"
 "time"

 "configflow/internal/lifecycle"
)

func operationKeys(routers []*Router) [][2]string {
 keys := make([][2]string, 0, len(routers))
 for _, r := range routers { if r != nil { keys = append(keys, [2]string{r.Type, strings.TrimRight(r.Endpoint, "/")}) } }
 slices.SortFunc(keys, func(a,b [2]string) int { if c:=strings.Compare(a[0],b[0]);c!=0{return c};return strings.Compare(a[1],b[1]) })
 return slices.Compact(keys)
}

// RouterOperationLocks acquires the shared route-editor device locks as one
// sorted group. Cloned connections have one lock. Only this API may acquire
// sibling device locks: the returned context records the device level so any
// subsequent external same-level acquisition remains an order violation.
func RouterOperationLocks(ctx context.Context, routers []*Router, write bool) (context.Context, func(), error) {
 limit:=time.Duration(0)
 if write { ctx=lifecycle.Detach(ctx); limit=10*time.Minute }
 if err:=ctx.Err();err!=nil{return ctx,nil,err}
 parent:=ctx
 var releases []func()
 var once sync.Once
 release:=func(){once.Do(func(){for i:=len(releases)-1;i>=0;i--{releases[i]()}})}
 for _,key:=range operationKeys(routers) {
  // All members check the original ancestors, never a sibling. Sorted keys
  // provide the internal deadlock order; no checker exemption escapes here.
  held,unlock,err:=routeLocks.Lock(parent,key,limit)
  if err!=nil {release();if write&&parent.Err()==nil{return parent,nil,rosError(KindConflict,messageEditBusy)};return parent,nil,err}
  releases=append(releases,unlock);ctx=held
  if err:=parent.Err();err!=nil{release();return parent,nil,err}
 }
 return ctx,release,nil
}
