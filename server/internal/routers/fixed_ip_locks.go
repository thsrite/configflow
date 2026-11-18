package routers

import (
 "context"
 "strings"
 "time"

 "configflow/internal/lifecycle"
)

// RouterOperationLocks acquires a batch of existing R06 locks. All multi-lock
// claims are try-only; a busy claim releases the partial set before waiting on
// that one device. No same-level blocking lock is taken under another L2.
// The caller must already hold every corresponding L1 job lock.
func RouterOperationLocks(ctx context.Context,rs []*Router)(context.Context,func(),error){ctx=lifecycle.Detach(ctx);keys:=[][2]string{};seen:=map[[2]string]bool{};for _,r:=range rs{key:=[2]string{r.Type,strings.TrimRight(r.Endpoint,"/")};if !seen[key]{keys=append(keys,key);seen[key]=true}};deadline:=time.Now().Add(10*time.Minute)
 for {if err:=ctx.Err();err!=nil{return ctx,nil,err};held:=ctx;releases:=[]func(){};release:=func(){for i:=len(releases)-1;i>=0;i--{releases[i]()}};var busy *[2]string
  for _,key:=range keys{next,unlock,ok:=routeLocks.TryLock(held,key);if !ok{copy:=key;busy=&copy;break};held=next;releases=append(releases,unlock)};if busy==nil{return held,release,nil};release();remaining:=time.Until(deadline);if remaining<=0{return ctx,nil,rosError(KindConflict,messageEditBusy)};_,unlock,err:=routeLocks.Lock(ctx,*busy,remaining);if err!=nil{if ctx.Err()!=nil{return ctx,nil,ctx.Err()};return ctx,nil,rosError(KindConflict,messageEditBusy)};unlock()
 }
}
