package routers

import (
 "context"
 "errors"
 "reflect"
 "sync"
 "testing"
 "time"

 "configflow/internal/lockx"
)

func TestOperationKeys(t *testing.T) {
 a:=&Router{RouterSettings:RouterSettings{Type:"routeros",Endpoint:"https://router.example/"}}
 b:=&Router{RouterSettings:RouterSettings{Type:"ikuai",Endpoint:"https://other.example"}}
 clone:=*a;clone.Endpoint="https://router.example"
 want:=[][2]string{{"ikuai",b.Endpoint},{"routeros",clone.Endpoint}}
 for _,rows:=range [][]*Router{{a,b,&clone,nil},{&clone,b,a}}{if got:=operationKeys(rows);!reflect.DeepEqual(got,want){t.Fatalf("keys=%v",got)}}
}

func TestRouterOperationGroupCancellation(t *testing.T) {
 a:=&Router{RouterSettings:RouterSettings{Type:"routeros",Endpoint:"https://a.example"}}
 b:=&Router{RouterSettings:RouterSettings{Type:"routeros",Endpoint:"https://b.example"}}
 _,unlock,err:=RouterOperationLock(context.Background(),*b,false);if err!=nil{t.Fatal(err)};defer unlock()
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 done:=make(chan error,1)
 go func(){_,release,err:=RouterOperationLocks(ctx,[]*Router{b,a},false);if release!=nil{release()};done<-err}()
 deadline:=time.NewTimer(time.Second);defer deadline.Stop()
 for !routeLocks.Held(operationKeys([]*Router{a})[0]) {select{case<-deadline.C:t.Fatal("first lock not held");default:time.Sleep(time.Millisecond)}}
 cancel()
 if err:=<-done;!errors.Is(err,context.Canceled){t.Fatalf("err=%v",err)}
 if routeLocks.Held(operationKeys([]*Router{a})[0]){t.Fatal("partial group leaked")}
}

func TestRouterOperationGroupOppositeOrder(t *testing.T) {
 a:=&Router{RouterSettings:RouterSettings{Type:"routeros",Endpoint:"https://a.example"}}
 b:=&Router{RouterSettings:RouterSettings{Type:"routeros",Endpoint:"https://b.example"}}
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
 before:=lockx.Violations()
 var wg sync.WaitGroup
 for _,rows:=range [][]*Router{{a,b},{b,a}}{wg.Add(1);go func(){defer wg.Done();for range 30 {_,release,err:=RouterOperationLocks(ctx,rows,false);if err!=nil{t.Error(err);return};release()}}()}
 wg.Wait()
 if delta:=lockx.Violations()-before;delta!=0{t.Fatalf("group violations=%d",delta)}
 // The internal group exception must not permit external equal-level nesting.
 held,release,err:=RouterOperationLocks(ctx,[]*Router{a},false);if err!=nil{t.Fatal(err)};defer release()
 before=lockx.Violations()
 _,unlock,err:=RouterOperationLock(held,*b,false);if err!=nil{t.Fatal(err)};unlock()
 if lockx.Violations()-before!=1{t.Fatal("outside nesting escaped checker")}
}
