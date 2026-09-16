package ludo

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"github.com/heroiclabs/nakama-common/runtime"
)

func ludoCreatePartyMatch(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string,error) {
	caller,_:=ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	var req struct { Users []string `json:"users"` }
	if caller=="" || len(payload)>2048 || json.Unmarshal([]byte(payload),&req)!=nil || (len(req.Users)!=2 && len(req.Users)!=4) { return "",runtime.NewError("invalid roster",3) }
	seen:=map[string]bool{}
	for _,id:=range req.Users { if seen[id] || id=="" { return "",runtime.NewError("invalid roster",3) }; seen[id]=true }
	if !seen[caller] { return "",runtime.NewError("creator must participate",7) }
	sort.Strings(req.Users)
	mode:="ludo_2p"; if len(req.Users)==4 { mode="ludo_4p" }
	id,err:=nk.MatchCreate(ctx,ludoBotMatchModule,map[string]interface{}{"protocol":2,"mode":mode,"human_user_id":caller,"users":req.Users})
	if err!=nil { return "",err }; data,_:=json.Marshal(map[string]string{"match_id":id}); return string(data),nil
}

func ludoAuthoritativeMatched(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, entries []runtime.MatchmakerEntry) (string,error) {
	if len(entries)!=2 && len(entries)!=4 { return "",nil }
	ids:=[]string{}
	for _,entry:=range entries {
		if entry.GetProperties()["ludoAuthority"]!="2" { return "",nil }
		ids=append(ids,entry.GetPresence().GetUserId())
	}
	sort.Strings(ids)
	mode:="ludo_2p"; if len(ids)==4 { mode="ludo_4p" }
	return nk.MatchCreate(ctx,ludoBotMatchModule,map[string]interface{}{"protocol":2,"mode":mode,"human_user_id":ids[0],"users":ids})
}
