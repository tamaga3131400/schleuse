<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ArrowUpRight, ArrowRight, ShieldCheck, Network, KeyRound, Users, Activity, Settings2, Plus, X, Play, Check, Copy, RefreshCw, LogOut, LockKeyhole, CircleHelp, Download, Pencil, Trash2, LayoutGrid, Code2 } from '@lucide/vue'
type HeaderValue={name:string;value:string}
type Connection={id:string;name:string;tenant:string;url:string;auth:string;header:string;prefix:string;headers:HeaderValue[]|null;allow_private:boolean;secret_set:boolean}
type Action={id:string;name:string;slug:string;connection_id:string;method:string;path:string;fields:string[]|null;query:string[]|null;body_template:string;body_format:string;active:boolean}
type User={id:string;name:string;username:string;tenant:string;role:string;active:boolean;grants:string[]}
type Event={id:number;at:string;action:string;source:string;status:number;duration_ms:number}
type IntegrationToken={id:string;name:string;action_ids:string[];expires_at:number}
type TemplateAction={name:string;slug:string;method:string;path:string;query:string[];fields:string[];body_template:string;body_format:string;note:string}
type Template={id:string;name:string;category:string;description:string;status:string;connection:{url:string;url_hint:string;auth:string;header:string;prefix:string;headers:HeaderValue[];secret_label:string};help:string;docs:string;actions:TemplateAction[]}
const state=reactive<{connections:Connection[];actions:Action[];users:User[];events:Event[];integration_tokens:IntegrationToken[]}>({connections:[],actions:[],users:[],events:[],integration_tokens:[]})
const ready=ref(false),logged=ref(false),setup=ref(false),demo=ref(false),csrf=ref(''),page=ref('connections'),modal=ref(''),busy=ref(false),error=ref(''),notice=ref(''),apiURL=ref('http://127.0.0.1:18080'),version=ref('')
const login=reactive({username:'',password:'',name:'Administrator',code:''})
const form=reactive<any>({})
const pages=[{id:'connections',name:'API-Verbindungen',icon:Network},{id:'actions',name:'Aktionen',icon:KeyRound},{id:'tokens',name:'API-Tokens',icon:LockKeyhole},{id:'users',name:'Zugänge & Freigaben',icon:Users},{id:'events',name:'Diagnose',icon:Activity},{id:'settings',name:'Betrieb',icon:Settings2}]
const title=computed(()=>pages.find(x=>x.id===page.value)?.name||'Arbeitsbereich')
const clients=computed(()=>state.users.filter(u=>u.role==='client'))
const availableActions=computed(()=>state.actions.filter(a=>tenantOf(a)===form.tenant))
const tokenActions=computed(()=>state.actions.filter(action=>action.active))

// Hilfsfunktionen
const methodsWithBody=['POST','PUT','PATCH']
function authMode(c:{header:string;auth?:string}){return c.header===''?'none':(c.auth||'header')}
function authLabel(c:Connection){const m=authMode(c);return m==='none'?'Ohne Anbieter-Schlüssel':m==='basic'?'Benutzername + Passwort':m==='query'?'URL-Parameter '+c.header:'Header '+c.header}
function connectionOf(a:Action){return state.connections.find(c=>c.id===a.connection_id)}
function tenantOf(a:Action){return connectionOf(a)?.tenant||''}
function pathParams(path:string){return [...path.split('?')[0].matchAll(/\{([a-z][a-z0-9_]*)\}/g)].map(m=>m[1])}
function paramsOf(a:{path:string;query:string[]|null}){return [...pathParams(a.path),...(a.query||[])]}
function templateVars(s:string){return [...new Set([...String(s||'').matchAll(/\{\{\s*([a-z][a-z0-9_]*)\s*\}\}/g)].map(m=>m[1]))]}
function adminPlaceholders(s:string){return [...new Set(s.match(/\{[A-Z][A-Z0-9_]+\}/g)||[])]}
function list(text:string){return String(text||'').split(',').map(s=>s.trim()).filter(Boolean)}
function actionURL(a:Action){return apiURL.value+'/actions/'+a.slug}
function actionLabel(id:string){
  const action=state.actions.find(item=>item.id===id)
  return action ? [action.name,tenantOf(action)].filter(Boolean).join(' · ') : 'Aktion entfernt'
}
function slugPart(s:string){return s.toLowerCase().replace(/[^a-z0-9]+/g,'-').replace(/^-+|-+$/g,'')}
function parseHeaders(text:string):HeaderValue[]{
  return String(text||'').split('\n').map(l=>l.trim()).filter(Boolean).map(l=>{
    const i=l.indexOf(':')
    if(i<1) throw new Error('Zusatz-Header bitte als „Name: Wert“ eingeben, einen pro Zeile.')
    return {name:l.slice(0,i).trim(),value:l.slice(i+1).trim()}
  })
}
function expiry(t:IntegrationToken){
  if(!t.expires_at) return 'Unbegrenzt (bis zum Widerruf)'
  const d=new Date(t.expires_at*1000)
  return d.toLocaleDateString('de-DE')+(d.getTime()<Date.now()?' · abgelaufen':'')
}
async function copyText(value:string,done='In die Zwischenablage kopiert.'){try{await navigator.clipboard.writeText(value);notice.value=done}catch{notice.value='Bitte markieren und manuell kopieren.'}}

async function request(path:string,method='GET',body?:unknown){const r=await fetch('/api'+path,{method,headers:{'Content-Type':'application/json','X-CSRF-Token':csrf.value},body:body===undefined?undefined:JSON.stringify(body)});const data=await r.json();if(!r.ok){if(r.status===401)logged.value=false;throw new Error(data.error||'Anfrage fehlgeschlagen.')}return data}
async function load(){const s=await request('/state');Object.assign(state,s)}
async function session(){const s=await request('/session');csrf.value=s.csrf;demo.value=s.demo;apiURL.value=s.api_url;logged.value=true;await load()}
onMounted(async()=>{try{const s=await request('/status');setup.value=s.setup_required;demo.value=s.demo;version.value=s.version||'';try{await session()}catch{}}catch(e:any){error.value=e.message}finally{ready.value=true}})
async function signIn(){busy.value=true;error.value='';try{if(setup.value){await request('/setup','POST',login);setup.value=false;notice.value='Einrichtung abgeschlossen. Jetzt mit deinem Passwort anmelden.'}else{const s=await request('/login','POST',{username:login.username,password:login.password});csrf.value=s.csrf;login.password='';await session()}}catch(e:any){error.value=e.message}finally{busy.value=false}}
async function logout(){await request('/logout','POST',{});closeToken();logged.value=false;csrf.value=''}

// Verbindungen, Aktionen und Zugänge
watch(()=>form.tenant,()=>{if(modal.value==='user')form.grants=[]})
function open(kind:string,item?:any){
  error.value='';notice.value='';Object.keys(form).forEach(k=>delete form[k])
  if(kind==='connection')Object.assign(form,item?{...item,auth:authMode(item),headersText:(item.headers||[]).map((h:HeaderValue)=>h.name+': '+h.value).join('\n'),secret:'',basic_user:''}:{name:'',tenant:'kunde-a',url:'https://',auth:'header',header:'Authorization',prefix:'Bearer ',headersText:'',secret:'',basic_user:'',allow_private:false})
  if(kind==='action')Object.assign(form,item?{...item,queryText:(item.query||[]).join(', '),fieldsText:(item.fields||[]).join(', '),bodyMode:item.body_template?'template':'fields',body_format:item.body_format||''}:{name:'',slug:'',connection_id:state.connections[0]?.id||'',method:'GET',path:'/',queryText:'',fieldsText:'',bodyMode:'fields',body_template:'',body_format:'',active:false})
  if(kind==='user')Object.assign(form,{name:'',username:'',tenant:state.connections[0]?.tenant||'kunde-a',password:'',grants:[]})
  modal.value=kind
}
function connectionPayload(){
  const auth=form.auth
  return {id:form.id,name:form.name,tenant:form.tenant,url:form.url,auth,
    header:auth==='none'?'':auth==='basic'?'Authorization':form.header,
    prefix:auth==='header'?form.prefix:auth==='basic'?'Basic ':'',
    headers:parseHeaders(form.headersText),secret:auth==='none'?'':form.secret,basic_user:auth==='basic'?form.basic_user:'',
    allow_private:auth==='none'?false:!!form.allow_private}
}
function actionPayload(){
  const write=methodsWithBody.includes(form.method), template=write&&form.bodyMode==='template'
  return {id:form.id,name:form.name,slug:form.slug,connection_id:form.connection_id,method:form.method,path:form.path,query:list(form.queryText),
    fields:write&&!template?list(form.fieldsText):[],body_template:template?form.body_template:'',body_format:write?form.body_format:'',active:!!form.active}
}
async function save(){
  busy.value=true;error.value=''
  try{
    if(modal.value==='connection'){const p=connectionPayload();await request('/connections'+(form.id?'/'+form.id:''),form.id?'PUT':'POST',p)}
    else if(modal.value==='action'){const p=actionPayload();await request('/actions'+(form.id?'/'+form.id:''),form.id?'PUT':'POST',p)}
    else await request('/users','POST',form)
    const created=!form.id&&modal.value==='action'
    if(form.secret)form.secret='';if(form.password)form.password=''
    modal.value='';notice.value=created?'Gespeichert. Neue Aktionen sind zunächst inaktiv – testen und dann bewusst aktivieren.':'Gespeichert.';await load()
  }catch(e:any){error.value=e.message}finally{busy.value=false}
}
async function toggle(a:Action){try{await request('/actions/'+a.id,'PUT',{...a,query:a.query||[],fields:a.fields||[],active:!a.active});await load()}catch(e:any){error.value=e.message}}
async function remove(kind:string,id:string){if(!confirm(kind==='users'?'Zugang sperren? Bestehende Access-Tokens werden damit ungültig.':kind==='actions'?'Aktion löschen? Tokens verlieren diese Freigabe sofort.':'Diesen Eintrag löschen?'))return;try{await request('/'+kind+'/'+id,'DELETE');await load()}catch(e:any){error.value=e.message}}
function actionFor(c:Connection){return state.actions.find(a=>a.connection_id===c.id&&a.method==='GET')}
async function exportConfig(){const data=await request('/export');const u=URL.createObjectURL(new Blob([JSON.stringify(data,null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=u;a.download='schleuse-verbindungen.json';a.click();URL.revokeObjectURL(u)}
function date(s:string){return new Date(s).toLocaleString('de-DE',{day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit'})}

// Test einer Leseaktion
const tester=reactive<{action:Action|null;values:Record<string,string>;result:any;busy:boolean;error:string}>({action:null,values:{},result:null,busy:false,error:''})
function openTest(a:Action){
  Object.assign(tester,{action:a,values:Object.fromEntries(paramsOf(a).map(p=>[p,''])),result:null,busy:false,error:''})
  if(!pathParams(a.path).length) runTest()
}
async function runTest(){
  if(!tester.action) return
  tester.busy=true;tester.error='';tester.result=null
  try{
    const r=await request('/actions/'+tester.action.id+'/test','POST',{params:tester.values})
    try{r.pretty=JSON.stringify(JSON.parse(r.preview),null,2)}catch{r.pretty=r.preview}
    tester.result=r;await load()
  }catch(e:any){tester.error=e.message}finally{tester.busy=false}
}

// Vorlagen-Assistent
const wizard=reactive<{open:boolean;list:Template[];search:string;chosen:Template|null;form:any;actions:any[];busy:boolean;error:string}>({open:false,list:[],search:'',chosen:null,form:{},actions:[],busy:false,error:''})
const filteredTemplates=computed(()=>{
  const q=wizard.search.trim().toLowerCase()
  const items=wizard.list.filter(t=>!q||(t.name+' '+t.category+' '+t.description).toLowerCase().includes(q))
  const groups:Record<string,Template[]>={}
  items.forEach(t=>(groups[t.category]=groups[t.category]||[]).push(t))
  return groups
})
const statusLabel:Record<string,string>={getestet:'Getestet',teilweise:'Teilweise getestet',doku:'Nach Anbieter-Doku'}
async function openTemplates(){
  error.value='';notice.value=''
  Object.assign(wizard,{open:true,search:'',chosen:null,error:'',busy:false})
  if(!wizard.list.length){try{wizard.list=(await request('/templates')).templates}catch(e:any){wizard.error=e.message}}
}
function chooseTemplate(t:Template){
  wizard.chosen=t
  wizard.form={name:t.name,tenant:state.connections[0]?.tenant||'kunde-a',url:t.connection.url,secret:'',basic_user:''}
  wizard.actions=t.actions.map(a=>({...a,selected:true,slug_base:a.slug,slug:a.slug}))
  updateSlugs()
}
function updateSlugs(){const p=slugPart(wizard.form.tenant||'');wizard.actions.forEach(a=>{a.slug=((p?p+'-':'')+a.slug_base).slice(0,80)})}
watch(()=>wizard.form.tenant,updateSlugs)
const wizardMissing=computed(()=>{
  if(!wizard.chosen) return []
  const texts=[wizard.form.url,...wizard.actions.filter(a=>a.selected).flatMap(a=>[a.path,a.body_template||''])]
  return [...new Set(texts.flatMap(adminPlaceholders))]
})
async function createFromTemplate(){
  const t=wizard.chosen;if(!t)return
  wizard.error=''
  if(wizardMissing.value.length){wizard.error='Bitte zuerst ersetzen: '+wizardMissing.value.join(', ');return}
  wizard.busy=true
  try{
    const c=t.connection
    const {id}=await request('/connections','POST',{name:wizard.form.name,tenant:wizard.form.tenant,url:wizard.form.url,auth:c.auth,header:c.auth==='none'?'':c.header,prefix:c.prefix,headers:c.headers||[],secret:wizard.form.secret,basic_user:wizard.form.basic_user,allow_private:false})
    const failed:string[]=[];let created=0
    for(const a of wizard.actions.filter(a=>a.selected)){
      try{await request('/actions','POST',{name:a.name,slug:a.slug,connection_id:id,method:a.method,path:a.path,query:a.query||[],fields:a.body_template?[]:(a.fields||[]),body_template:a.body_template||'',body_format:a.body_format||'',active:false});created++}
      catch(e:any){failed.push(a.name+': '+e.message)}
    }
    wizard.open=false;await load();page.value='actions'
    notice.value=`Verbindung „${wizard.form.name}“ und ${created} Aktion(en) angelegt. Jetzt Leseaktionen testen, gewünschte Aktionen aktivieren und unter API-Tokens einen Token erstellen.`
    if(failed.length) error.value='Nicht angelegt: '+failed.join(' · ')
  }catch(e:any){wizard.error=e.message}finally{wizard.busy=false}
}

// API-Tokens
const tokenForm=reactive({name:'',action_ids:[] as string[],mode:'unlimited',days:90})
const tokenDialog=ref(false)
const issued=ref<(IntegrationToken & {token:string}) | null>(null)
const tokenCopied=ref(false)
const tokenError=ref('')
const tokenTenant=computed(()=>{const first=state.actions.find(a=>a.id===tokenForm.action_ids[0]);return first?tenantOf(first):''})
const issuedActions=computed(()=>(issued.value?.action_ids||[]).map(id=>state.actions.find(a=>a.id===id)).filter(Boolean) as Action[])
function openToken(){
  Object.assign(tokenForm,{name:'',action_ids:[],mode:'unlimited',days:90})
  tokenError.value='';tokenCopied.value=false;issued.value=null;tokenDialog.value=true
}
async function issueToken(){
  tokenError.value=''
  if(!tokenForm.action_ids.length){tokenError.value='Mindestens eine Aktion auswählen.';return}
  const days=tokenForm.mode==='unlimited'?0:Math.round(Number(tokenForm.days))
  if(tokenForm.mode==='limited'&&(!days||days<1)){tokenError.value='Laufzeit in Tagen angeben.';return}
  if(days&&days<30&&!confirm(`Der Token läuft schon nach ${days} Tag${days===1?'':'en'} ab. Danach schlagen alle Aufrufe damit fehl, bis ein neuer Token eingetragen ist.\n\nIst diese kurze Laufzeit wirklich gewollt?`))return
  busy.value=true
  try{
    const result=await request('/integration-tokens','POST',{name:tokenForm.name,action_ids:tokenForm.action_ids,days}) as IntegrationToken & {token:string}
    const {token,...metadata}=result
    state.integration_tokens.unshift(metadata)
    issued.value={...metadata,token}
  }catch(e:any){tokenError.value=e.message}finally{busy.value=false}
}
function closeToken(){if(busy.value)return;issued.value=null;tokenDialog.value=false}
async function copyToken(){if(!issued.value)return;try{await navigator.clipboard.writeText(issued.value.token);tokenCopied.value=true}catch{tokenError.value='Bitte den Token im Feld markieren und kopieren.'}}
async function revokeToken(token:IntegrationToken){
  if(!confirm('Diesen Token widerrufen? Aufrufe mit diesem Token werden ab sofort abgelehnt.')) return
  try{await request('/integration-tokens/'+token.id,'DELETE');state.integration_tokens=state.integration_tokens.filter(item=>item.id!==token.id)}catch(e:any){error.value=e.message}
}

// Client-Vorlagen für Ninox, Make und curl
const snip=reactive<{action:Action|null;token:string;tab:string}>({action:null,token:'',tab:'ninox'})
function openSnippet(a:Action,token=''){Object.assign(snip,{action:a,token,tab:'ninox'})}
const snippet=computed(()=>{
  const a=snip.action;if(!a)return ''
  const token=snip.token||'SCHLEUSE_TOKEN', url=actionURL(a), params=paramsOf(a), fields=a.fields||[], write=methodsWithBody.includes(a.method)
  if(snip.tab==='ninox'){
    let urlExpr=JSON.stringify(url)
    params.forEach((p,i)=>{urlExpr+=` + "${i?'&':'?'}${p}=" + urlEncode("WERT")`})
    const lines=[`let token := ${JSON.stringify(token)};`,`let url := ${urlExpr};`]
    if(write&&fields.length){lines.push('let body := {');fields.forEach((f,i)=>lines.push(`    ${f}: "WERT"${i<fields.length-1?',':''}`));lines.push('};')}
    const headers=write?'{ Authorization: "Bearer " + token, "Content-Type": "application/json" }':'{ Authorization: "Bearer " + token }'
    lines.push('let response := do as server',`    http(${JSON.stringify(a.method)}, url, ${headers}, ${write?(fields.length?'body':'{}'):'null'})`,'end;','let result := response.result;','if response.error or result.error then','    alert("Schleuse-Aufruf fehlgeschlagen: " + text(response))','else','    alert(text(result))','end')
    return lines.join('\n')
  }
  if(snip.tab==='make'){
    const lines=['Modul: HTTP → „Make a request“',`URL: ${url}`,`Methode: ${a.method}`,`Header: Authorization = Bearer ${token}`]
    if(params.length)lines.push(`Query-Parameter: ${params.map(p=>p+' = WERT').join(', ')}`)
    if(write){lines.push('Body-Typ: JSON (application/json)');lines.push('Body: '+JSON.stringify(Object.fromEntries(fields.map(f=>[f,'WERT']))))}
    lines.push('','Tipp: Den Token in Make als Verbindung vom Typ „API key“ (Header Authorization, Wert „Bearer …“) speichern.')
    return lines.join('\n')
  }
  const query=params.length?'?'+params.map(p=>p+'=WERT').join('&'):''
  const lines=[`curl -X ${a.method} '${url}${query}' \\`,`  -H 'Authorization: Bearer ${token}'`]
  if(write){lines[lines.length-1]+=' \\';lines.push(`  -H 'Content-Type: application/json' \\`,`  -d '${JSON.stringify(Object.fromEntries(fields.map(f=>[f,'WERT'])))}'`)}
  return lines.join('\n')
})
</script>

<template>
<div v-if="!ready" class="loading">Schleuse wird geöffnet …</div>
<div v-else-if="!logged" class="login-layout">
 <section class="login-story"><div class="brand"><div class="brandmark"><i></i><i></i><i></i></div>schleuse<span>LOCAL</span></div><div><p class="eyebrow light">DEIN ZUGANG. DEINE KONTROLLE.</p><h1>APIs verbinden.<br>Schlüssel behalten.</h1><p>Ein sicherer Übergang zwischen deinen Anwendungen und den Diensten, die sie brauchen.</p><div class="story-path"><span>Deine App</span><ArrowRight/><ShieldCheck/><ArrowRight/><span>Ziel-API</span></div></div><small>Privat betrieben · Go + Vue + SQLite</small></section>
 <section class="login-side"><form class="login-card" @submit.prevent="signIn"><div class="icon-tile"><LockKeyhole/></div><p class="eyebrow">{{setup?'ERSTE EINRICHTUNG':'DEIN ARBEITSBEREICH'}}</p><h2>{{setup?'Willkommen an Bord.':'Schön, dass du da bist.'}}</h2><p class="muted">{{setup?'Richte den ersten Administrator ein. Der Einrichtungscode liegt im Datenordner.':'Melde dich an, um Verbindungen und Freigaben zu verwalten.'}}</p><div v-if="demo" class="demo-note">Lokaler Pilot mit Dummy-Daten.<br><strong>demo</strong> / <strong>Lokal-testen-2026!</strong></div><label v-if="setup">Einrichtungscode<input v-model="login.code" type="password" required autocomplete="off"></label><label>Benutzername<input v-model="login.username" required autocomplete="username" placeholder="Dein Benutzername"></label><label>Passwort<input v-model="login.password" type="password" :minlength="setup?12:undefined" required :autocomplete="setup?'new-password':'current-password'"></label><p v-if="error" class="alert error" role="alert">{{error}}</p><p v-if="notice" class="alert">{{notice}}</p><button class="primary wide" :disabled="busy">{{busy?'Einen Moment …':setup?'Administrator einrichten':'Arbeitsbereich öffnen'}}<ArrowRight :size="17"/></button><small>Die Verwaltung ist vom öffentlichen API-Zugang getrennt.</small></form></section>
</div>
<div v-else class="app-layout">
 <aside class="sidebar"><div class="brand"><div class="brandmark"><i></i><i></i><i></i></div>schleuse</div><div class="workspace"><span class="workspace-icon">S</span><div><strong>Mein Gateway</strong><small>{{demo?'Lokaler Pilot':'Privater Arbeitsbereich'}}</small></div><span class="dot"></span></div><p class="nav-label">VERWALTUNG</p><nav><button v-for="p in pages" :key="p.id" :class="{selected:page===p.id}" @click="page=p.id;error='';notice=''"><component :is="p.icon" :size="18"/>{{p.name}}<span v-if="p.id==='connections'" class="nav-count">{{state.connections.length}}</span></button></nav><div class="sidebar-bottom"><div class="privacy"><ShieldCheck :size="21"/><strong>Schlüssel bleiben hier.</strong><p>Serverseitig ergänzt.<br>Für deine Nutzer unsichtbar.</p></div><button class="logout" @click="logout"><LogOut :size="16"/> Abmelden</button></div></aside>
 <div class="main-shell"><header class="topbar"><span>Arbeitsbereich <span class="slash">/</span> {{title}}</span><div><span class="local-pill"><span class="dot"></span>{{demo?'Lokaler Testbetrieb':'Privates Gateway'}}</span><span class="avatar">A</span></div></header>
 <main><div class="page-title"><div><p class="eyebrow">DEINE SCHNITTSTELLEN, AN EINEM ORT</p><h1>{{title}}</h1><p class="muted">{{page==='connections'?'Verbinde Dienste. Gib Aktionen frei. Behalte die Zugangsdaten auf deinem Server.':page==='actions'?'Lege genau fest, welche Aufrufe deine Anwendungen ausführen dürfen.':page==='users'?'Persönliche Zugänge mit Benutzername und Passwort für Skripte, die sich selbst anmelden.':page==='tokens'?'Tokens für Ninox, Make und Skripte – unbegrenzt oder befristet, jederzeit widerrufbar.':page==='events'?'Prüfe Antworten und Verbindungsfehler, ohne Zugangsdaten offenzulegen.':'Alles für deinen Betrieb und die Sicherung.'}}</p></div>
  <div v-if="page==='connections'" class="title-actions"><button class="secondary" @click="open('connection')"><Plus :size="16"/>Eigene Verbindung</button><button class="primary" @click="openTemplates"><LayoutGrid :size="17"/>Aus Vorlage</button></div>
  <button v-else-if="page==='actions'" class="primary" :disabled="!state.connections.length" @click="open('action')"><Plus :size="17"/>Aktion anlegen</button><button v-else-if="page==='users'" class="primary" @click="open('user')"><Plus :size="17"/>Zugang anlegen</button><button v-else-if="page==='tokens'" class="primary" :disabled="!tokenActions.length" @click="openToken"><Plus :size="17"/>Token erstellen</button><button v-else class="secondary" @click="load"><RefreshCw :size="16"/>Aktualisieren</button></div>
 <div v-if="error" class="alert error" role="alert">{{error}}<button @click="error=''" aria-label="Meldung schließen"><X :size="16"/></button></div><div v-if="notice" class="alert" role="status">{{notice}}<button @click="notice=''" aria-label="Meldung schließen"><X :size="16"/></button></div>
 <section class="stats"><article><div><span>Verbundene Dienste</span><strong>{{state.connections.length.toString().padStart(2,'0')}}</strong></div><Network :size="23"/></article><article><div><span>Aktive Aktionen</span><strong>{{state.actions.filter(a=>a.active).length.toString().padStart(2,'0')}}</strong></div><KeyRound :size="23"/></article><article><div><span>API-Tokens</span><strong>{{state.integration_tokens.length.toString().padStart(2,'0')}}</strong></div><LockKeyhole :size="23"/></article></section>

 <template v-if="page==='connections'">
  <div class="section-head"><h2>Deine Verbindungen <span>{{state.connections.length}}</span></h2><span class="muted">Zugangsdaten werden verschlüsselt gespeichert</span></div>
  <div v-if="!state.connections.length" class="empty"><Network :size="32"/><h3>Die erste Verbindung wartet.</h3><p>Wähle eine Vorlage für Ninox, Make, HubSpot, WooCommerce und viele mehr – oder hinterlege eine eigene API.</p><div class="row-actions"><button class="secondary" @click="open('connection')">Eigene Verbindung</button><button class="primary" @click="openTemplates">Aus Vorlage wählen</button></div></div>
  <div class="connection-grid"><article v-for="c in state.connections" :key="c.id" class="connection-card"><div class="card-top"><div class="connection-icon"><Network :size="23"/></div><span class="pill"><LockKeyhole :size="12"/>{{c.secret_set?'Schlüssel hinterlegt':'Öffentliche Ziel-API'}}</span><button class="icon-button" title="Verbindung bearbeiten" @click="open('connection',c)"><Pencil :size="16"/></button></div><h3>{{c.name}}</h3><p class="endpoint">{{c.url}}</p><div class="card-meta"><span>Kunde <b>{{c.tenant}}</b></span><span>Anmeldung <b>{{authLabel(c)}}</b></span><span v-if="(c.headers||[]).length">Zusatz-Header <b>{{(c.headers||[]).map(h=>h.name).join(', ')}}</b></span></div><div class="card-bottom"><span>{{state.actions.filter(a=>a.connection_id===c.id).length}} Aktionen konfiguriert</span><button :disabled="!actionFor(c)" @click="openTest(actionFor(c)!)">Verbindung testen<ArrowUpRight :size="15"/></button></div></article></div>
  <div class="next-step"><div class="round-icon"><ArrowRight :size="20"/></div><div><h3>Eine Verbindung. Klare Freigaben.</h3><p>Teste die Aktionen, aktiviere die benötigten und erstelle einen API-Token für Ninox, Make oder dein Skript.</p></div><button class="secondary" @click="page='actions'">Zu den Aktionen<ArrowRight :size="16"/></button></div>
 </template>

 <template v-if="page==='actions'">
  <section class="table-panel"><table><thead><tr><th>Aktion</th><th>Ziel</th><th>Eingaben</th><th>Status</th><th>Bedienung</th></tr></thead><tbody>
   <tr v-for="a in state.actions" :key="a.id">
    <td><strong>{{a.name}}</strong><small>{{tenantOf(a)}} · {{connectionOf(a)?.name}}</small><button class="copy-line" @click="copyText(actionURL(a))">/actions/{{a.slug}} <Copy :size="12"/></button></td>
    <td><span class="method">{{a.method}}</span> <code>{{a.path}}</code></td>
    <td class="inputs"><span v-if="paramsOf(a).length">URL: {{paramsOf(a).join(', ')}}</span><span v-if="(a.fields||[]).length">Body: {{(a.fields||[]).join(', ')}}</span><span v-if="a.body_template">Vorlage{{a.body_format==='form'?' · Formular':''}}</span><span v-if="!paramsOf(a).length&&!(a.fields||[]).length&&!a.body_template" class="muted">keine</span></td>
    <td><button class="status-toggle" :class="{on:a.active}" @click="toggle(a)"><span></span>{{a.active?'Aktiv':'Inaktiv'}}</button></td>
    <td class="row-actions"><button class="secondary small" :disabled="a.method!=='GET'" :title="a.method!=='GET'?'Schreibaktionen testest du mit einem Token aus deinem Client.':''" @click="openTest(a)"><Play :size="13"/>Testen</button><button class="secondary small" @click="openSnippet(a)"><Code2 :size="13"/>Vorlage</button><button class="icon-button" title="Aktion bearbeiten" @click="open('action',a)"><Pencil :size="16"/></button><button class="icon-button danger" aria-label="Aktion löschen" @click="remove('actions',a.id)"><Trash2 :size="16"/></button></td>
   </tr></tbody></table><div v-if="!state.actions.length" class="empty">Noch keine Aktion. Lege eine Verbindung aus einer Vorlage an oder erstelle eine eigene Aktion.</div></section>
  <p class="footnote">Platzhalter wie <code>{id}</code> im Zielpfad füllt der Client über <code>?id=…</code>. Freigegebene Query-Parameter werden weitergereicht, alle anderen abgelehnt. Bei einer Body-Vorlage liefert der Client nur die Werte der <code>&#123;&#123;platzhalter&#125;&#125;</code>. Antworten: JSON oder kurzer Text bis 2 MiB, Zeitlimit 30 Sekunden.</p>
 </template>

 <template v-if="page==='tokens'">
  <section class="table-panel"><table><thead><tr><th>Name</th><th>Aktionen</th><th>Gültig bis</th><th></th></tr></thead><tbody>
   <tr v-for="token in state.integration_tokens" :key="token.id"><td><strong>{{token.name}}</strong></td><td>{{token.action_ids.map(actionLabel).join(' · ')}}</td><td>{{expiry(token)}}</td><td><button class="secondary small" @click="revokeToken(token)">Widerrufen</button></td></tr>
  </tbody></table><div v-if="!state.integration_tokens.length" class="empty">{{tokenActions.length?'Noch keine API-Tokens.':'Aktiviere zuerst eine Aktion.'}}</div></section>
  <div class="info-panel"><ShieldCheck/><div><h3>Ein Token für ausgewählte Aktionen eines Kunden</h3><p>Ninox, Make oder dein Skript senden nur diesen Token als <code>Authorization: Bearer …</code>. Den Schlüssel der Ziel-API setzt Schleuse erst auf dem Server ein. Standard ist „unbegrenzt bis zum Widerruf“; ein Widerruf wirkt sofort.</p></div></div>
 </template>

 <template v-if="page==='users'"><section class="table-panel"><table><thead><tr><th>Person</th><th>Kunde</th><th>Freigaben</th><th>Status</th><th></th></tr></thead><tbody><tr v-for="u in clients" :key="u.id"><td><strong>{{u.name}}</strong><small>{{u.username}}</small></td><td>{{u.tenant}}</td><td>{{u.grants.length}} Aktionen</td><td><span class="pill" :class="{inactive:!u.active}">{{u.active?'Aktiv':'Gesperrt'}}</span></td><td><button class="secondary small" :disabled="!u.active" @click="remove('users',u.id)">Zugang sperren</button></td></tr></tbody></table><div v-if="!clients.length" class="empty">Noch keine persönlichen Zugänge. Für Ninox und Make reichen meist API-Tokens.</div></section><div class="info-panel"><ShieldCheck/><div><h3>Eine Anmeldung, gezielte Rechte.</h3><p>Nutzer melden sich an <code>{{apiURL}}/auth/login</code> an. Ihr Access-Token gilt 15 Minuten und erlaubt nur zugeordnete Aktionen. Gesperrte Nutzer verlieren sofort den Zugriff.</p><p v-if="demo">Beispielnutzer: <b>tester</b> / <b>Testzugang-2026!</b> · ausschließlich für die lokale Demo.</p></div></div></template>
 <template v-if="page==='events'"><section class="table-panel"><table><thead><tr><th>Zeitpunkt</th><th>Aktion</th><th>HTTP-Status</th><th>Quelle</th><th>Dauer</th></tr></thead><tbody><tr v-for="e in state.events" :key="e.id"><td>{{date(e.at)}}</td><td><strong>{{e.action}}</strong></td><td><span class="pill" :class="{inactive:e.status>=400}">{{e.status}}</span></td><td>{{e.source}}</td><td>{{e.duration_ms}} ms</td></tr></tbody></table><div v-if="!state.events.length" class="empty"><Activity :size="30"/><h3>Hier erscheinen echte Messungen.</h3><p>Teste eine Leseaktion oder führe einen freigegebenen Client-Aufruf aus.</p></div></section><p class="footnote">Die letzten 60 Weiterleitungen und Tests. Keine Anbieter-Schlüssel, Request-Bodies oder Zugangstokens in dieser Ansicht.</p></template>
 <template v-if="page==='settings'"><div class="settings-grid"><section class="panel"><div class="icon-tile"><ShieldCheck/></div><h2>Dein Gateway.</h2><p>Schleuse speichert Verbindungen, Aktionen und Tokens auf deinem Server. Es werden keine Daten an Dritte übertragen außer den freigegebenen Aufrufen an die Ziel-APIs.</p><dl><dt>Client-API</dt><dd>{{apiURL}}</dd><dt>Version</dt><dd>{{version||'–'}}</dd><dt>Speicher</dt><dd>SQLite · Schlüssel mit AES-GCM verschlüsselt</dd><dt>Anmeldung</dt><dd>API-Tokens je Kunde · persönliche Zugänge mit kurzlebigen Access-Tokens</dd></dl><button class="secondary" @click="exportConfig"><Download :size="16"/>Konfiguration ohne Schlüssel exportieren</button></section><section class="panel"><div class="icon-tile"><Settings2/></div><h2>Sicher betreiben.</h2><p>Der Verwaltungszugang bleibt separat. Nur die Client-API gehört hinter HTTPS ins Internet.</p><p>Ein vollständiges Backup benötigt Datenbank und Master-Key aus dem Datenordner. Der Metadatenexport ersetzt es nicht. Die Schritte stehen in der Installationsanleitung.</p><p class="muted">Noch nicht enthalten: automatische OAuth-Erneuerung (Google, Microsoft 365), Dateien und Streaming.</p></section></div></template>
 <footer><span><ShieldCheck :size="14"/>Dein Server. Deine Zugangsdaten.</span><span>Schleuse · Version {{version||'0.3'}}</span></footer>
 </main></div>

 <!-- Vorlagen-Assistent -->
 <div v-if="wizard.open" class="modal-backdrop" @click.self="wizard.open=false"><section class="modal wide" role="dialog" aria-modal="true" aria-labelledby="wizard-title">
  <div class="modal-head"><div><p class="eyebrow">{{wizard.chosen?'SCHRITT 2 VON 2':'SCHRITT 1 VON 2'}}</p><h2 id="wizard-title">{{wizard.chosen?wizard.chosen.name+' verbinden':'Vorlage wählen'}}</h2></div><button class="icon-button" @click="wizard.open=false" aria-label="Dialog schließen"><X/></button></div>
  <template v-if="!wizard.chosen">
   <label>Suchen<input v-model="wizard.search" placeholder="z. B. Ninox, Rechnung, CRM, Shop"></label>
   <template v-for="(items,category) in filteredTemplates" :key="category"><p class="category">{{String(category).toUpperCase()}}</p><div class="template-grid"><button v-for="t in items" :key="t.id" class="template-card" @click="chooseTemplate(t)"><strong>{{t.name}}</strong><small>{{t.description}}</small><span class="pill" :class="{inactive:t.status==='doku'}">{{statusLabel[t.status]}}</span></button></div></template>
   <p v-if="wizard.error" class="alert error" role="alert">{{wizard.error}}</p>
   <p class="muted">„Nach Anbieter-Doku“: nach der offiziellen Dokumentation erstellt, noch nicht mit einem echten Konto geprüft. Teste die Leseaktionen nach dem Anlegen.</p>
  </template>
  <form v-else @submit.prevent="createFromTemplate">
   <div class="help-box"><CircleHelp :size="15"/> {{wizard.chosen.help}} <a :href="wizard.chosen.docs" target="_blank" rel="noopener noreferrer">Dokumentation</a></div>
   <div class="form-row"><label>Name<input v-model="wizard.form.name" required></label><label>Kunde / Zuordnung<input v-model="wizard.form.tenant" required pattern="[A-Za-z0-9][A-Za-z0-9_-]{1,49}"></label></div>
   <label>Zieladresse<input v-model="wizard.form.url" required><span v-if="wizard.chosen.connection.url_hint" class="hint">{{wizard.chosen.connection.url_hint}}</span></label>
   <template v-if="wizard.chosen.connection.auth==='basic'"><div class="form-row"><label>Benutzername<input v-model="wizard.form.basic_user" required autocomplete="off"></label><label>Passwort<input v-model="wizard.form.secret" type="password" required autocomplete="new-password"></label></div></template>
   <label v-else-if="wizard.chosen.connection.auth!=='none'">{{wizard.chosen.connection.secret_label||'API-Schlüssel'}}<input v-model="wizard.form.secret" type="password" required minlength="8" autocomplete="new-password" placeholder="Wird nach dem Speichern nicht mehr angezeigt"></label>
   <p v-else class="muted">Diese API braucht keinen Anbieter-Schlüssel. Clients brauchen trotzdem einen Schleuse-Token.</p>
   <p v-if="(wizard.chosen.connection.headers||[]).length" class="hint">Feste Zusatz-Header: {{wizard.chosen.connection.headers.map(h=>h.name+': '+h.value).join(' · ')}}</p>
   <label>Aktionen</label>
   <div v-for="a in wizard.actions" :key="a.slug_base" class="action-pick">
    <label class="checkbox"><input v-model="a.selected" type="checkbox"><strong>{{a.name}}</strong> <span class="method">{{a.method}}</span></label>
    <template v-if="a.selected">
     <div class="form-row"><label>Zielpfad<input v-model="a.path" required></label><label>Aktionsname in der URL<input v-model="a.slug" required pattern="[a-z][a-z0-9-]{1,79}"></label></div>
     <p class="hint"><span v-if="paramsOf(a).length">Client übergibt per URL: {{paramsOf(a).join(', ')}}. </span><span v-if="(a.fields||[]).length">Body-Felder: {{a.fields.join(', ')}}. </span><span v-if="a.body_template">Client liefert im Body: {{templateVars(a.body_template).join(', ')||'nichts (feste Abfrage)'}}. </span>{{a.note}}</p>
     <details v-if="a.body_template"><summary>Body-Vorlage anpassen</summary><textarea v-model="a.body_template" spellcheck="false"></textarea></details>
    </template>
   </div>
   <p v-if="wizardMissing.length" class="alert">Noch zu ersetzen: {{wizardMissing.join(', ')}}</p>
   <p v-if="wizard.error" class="alert error" role="alert">{{wizard.error}}</p>
   <div class="modal-footer"><button type="button" class="secondary" @click="wizard.chosen=null">Zurück</button><button class="primary" :disabled="wizard.busy">{{wizard.busy?'Legt an …':'Verbindung und Aktionen anlegen'}}<Check :size="16"/></button></div>
  </form>
 </section></div>

 <!-- Test einer Leseaktion -->
 <div v-if="tester.action" class="modal-backdrop" @click.self="tester.action=null"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="test-title">
  <div class="modal-head"><div><p class="eyebrow">AKTION TESTEN</p><h2 id="test-title">{{tester.action.name}}</h2></div><button class="icon-button" @click="tester.action=null" aria-label="Dialog schließen"><X/></button></div>
  <form @submit.prevent="runTest">
   <p class="muted">Schleuse ruft die Ziel-API mit dem hinterlegten Schlüssel auf – genau wie bei einem Client-Aufruf.</p>
   <label v-for="p in paramsOf(tester.action)" :key="p">{{p}}{{pathParams(tester.action.path).includes(p)?' (Pflicht)':' (optional)'}}<input v-model="tester.values[p]" :required="pathParams(tester.action.path).includes(p)"></label>
   <p v-if="tester.error" class="alert error" role="alert">{{tester.error}}</p>
   <template v-if="tester.result"><p class="alert" :class="{error:!tester.result.ok}">HTTP {{tester.result.status}} · Quelle {{tester.result.source}} · {{tester.result.duration_ms}} ms</p><pre class="code">{{tester.result.pretty}}</pre></template>
   <div class="modal-footer"><button type="button" class="secondary" @click="tester.action=null">Schließen</button><button class="primary" :disabled="tester.busy">{{tester.busy?'Prüft …':tester.result?'Erneut testen':'Testen'}}<Play :size="15"/></button></div>
  </form>
 </section></div>

 <!-- API-Token -->
 <div v-if="tokenDialog" class="modal-backdrop" @click.self="closeToken"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="token-title">
  <div class="modal-head"><h2 id="token-title">{{issued?'API-Token erstellt':'API-Token erstellen'}}</h2><button type="button" class="icon-button" @click="closeToken" aria-label="Dialog schließen"><X/></button></div>
  <form v-if="!issued" @submit.prevent="issueToken">
   <label>Name<input v-model="tokenForm.name" required maxlength="100" placeholder="z. B. Ninox Kundenverwaltung"></label>
   <label>Erlaubte Aktionen</label>
   <div class="grants"><label v-for="a in tokenActions" :key="a.id" class="checkbox"><input v-model="tokenForm.action_ids" :value="a.id" type="checkbox" :disabled="!!tokenTenant&&tenantOf(a)!==tokenTenant">{{actionLabel(a.id)}}</label></div>
   <p class="hint">Ein Token gilt für Aktionen eines Kunden.</p>
   <label>Laufzeit</label>
   <div class="radio-row"><label class="checkbox"><input v-model="tokenForm.mode" type="radio" value="unlimited">Unbegrenzt (bis zum Widerruf)</label><label class="checkbox"><input v-model="tokenForm.mode" type="radio" value="limited">Befristet</label></div>
   <label v-if="tokenForm.mode==='limited'">Laufzeit in Tagen<input v-model.number="tokenForm.days" type="number" min="1" max="3650" step="1" required></label>
   <p v-if="tokenForm.mode==='limited'&&tokenForm.days<30" class="alert">Achtung: Nach {{tokenForm.days}} Tag(en) funktioniert die Integration nicht mehr, bis ein neuer Token eingetragen ist.</p>
   <p v-if="tokenError" class="alert error" role="alert">{{tokenError}}</p>
   <div class="modal-footer"><button type="button" class="secondary" @click="closeToken">Abbrechen</button><button class="primary" :disabled="busy">Token erstellen</button></div>
  </form>
  <div v-else>
   <p>Kopiere den Token jetzt. Schleuse zeigt ihn nur einmal an.</p>
   <label>API-Token<input :value="issued.token" readonly autocomplete="off" spellcheck="false"></label>
   <p class="hint">Gültig: {{expiry(issued)}}</p>
   <label>Freigegebene Aktionen</label>
   <div v-for="a in issuedActions" :key="a.id" class="action-pick"><strong>{{a.name}}</strong><button class="copy-line" type="button" @click="copyText(actionURL(a))">{{a.method}} {{actionURL(a)}} <Copy :size="12"/></button><button type="button" class="secondary small" @click="openSnippet(a,issued.token)"><Code2 :size="13"/>Vorlage für Ninox, Make, curl</button></div>
   <p v-if="tokenError" class="alert error" role="alert">{{tokenError}}</p>
   <div class="modal-footer"><button type="button" class="secondary" @click="closeToken">Schließen</button><button type="button" class="primary" @click="copyToken">{{tokenCopied?'Kopiert':'Token kopieren'}}<Copy :size="16"/></button></div>
  </div>
 </section></div>

 <!-- Client-Vorlage -->
 <div v-if="snip.action" class="modal-backdrop" @click.self="snip.action=null"><section class="modal wide" role="dialog" aria-modal="true" aria-labelledby="snip-title">
  <div class="modal-head"><div><p class="eyebrow">CLIENT-VORLAGE</p><h2 id="snip-title">{{snip.action.name}}</h2></div><button class="icon-button" @click="snip.action=null" aria-label="Dialog schließen"><X/></button></div>
  <div class="tabs"><button v-for="t in [['ninox','Ninox'],['make','Make'],['curl','curl']]" :key="t[0]" :class="{selected:snip.tab===t[0]}" @click="snip.tab=t[0]">{{t[1]}}</button></div>
  <pre class="code">{{snippet}}</pre>
  <p class="muted">{{snip.token?'Der Token ist bereits eingesetzt.':'SCHLEUSE_TOKEN durch einen API-Token ersetzen.'}} „WERT“ durch Felder oder feste Werte ersetzen. In Ninox gehört das Skript zum Beispiel in einen Button unter „Beim Klicken“.</p>
  <div class="modal-footer"><button type="button" class="secondary" @click="snip.action=null">Schließen</button><button class="primary" @click="copyText(snippet,'Vorlage kopiert.')">Kopieren<Copy :size="16"/></button></div>
 </section></div>

 <!-- Verbindung, Aktion, Zugang -->
 <div v-if="modal" class="modal-backdrop" @click.self="modal=''"><section class="modal" role="dialog" aria-modal="true" :aria-label="modal==='connection'?'API-Verbindung':modal==='action'?'Aktion':'Zugang anlegen'"><div class="modal-head"><div><p class="eyebrow">{{modal==='connection'?'DIENST VERBINDEN':modal==='action'?'ZUGRIFF EINGRENZEN':'PERSÖNLICHER ZUGANG'}}</p><h2>{{modal==='connection'?(form.id?'Verbindung bearbeiten':'Neue API-Verbindung'):modal==='action'?(form.id?'Aktion bearbeiten':'Neue Aktion'):'Neuen Zugang anlegen'}}</h2></div><button class="icon-button" @click="modal=''" aria-label="Dialog schließen"><X/></button></div><form @submit.prevent="save">
 <template v-if="modal==='connection'">
  <label>Name<input v-model="form.name" required placeholder="z. B. Kundenverwaltung"></label>
  <label>Kunde / Zuordnung<input v-model="form.tenant" required :disabled="!!form.id" pattern="[A-Za-z0-9][A-Za-z0-9_-]{1,49}"></label>
  <label>Zieladresse<input v-model="form.url" type="url" required placeholder="https://api.example.com/v1"></label>
  <label>Anmeldung bei der Ziel-API<select v-model="form.auth" :disabled="!!form.id"><option value="header">API-Schlüssel im Header</option><option value="basic">Benutzername + Passwort (Basic)</option><option value="query">API-Schlüssel als URL-Parameter</option><option value="none">Ohne Schlüssel (öffentliche HTTPS-API)</option></select></label>
  <p v-if="form.auth==='none'" class="muted">Ninox und andere Clients brauchen weiterhin einen freigegebenen Schleuse-Token.</p>
  <div v-if="form.auth==='header'" class="form-row"><label>Headername<input v-model="form.header" required></label><label>Präfix<input v-model="form.prefix" placeholder="Bearer "></label></div>
  <label v-if="form.auth==='query'">Name des URL-Parameters<input v-model="form.header" required placeholder="api_key"><span class="hint">Manche APIs erwarten den Schlüssel in der Adresse. Er kann dort in Server-Logs des Anbieters landen – Header sind besser, wenn möglich.</span></label>
  <div v-if="form.auth==='basic'" class="form-row"><label>Benutzername<input v-model="form.basic_user" :required="!form.id" autocomplete="off" :placeholder="form.id?'leer = behalten':''"></label><label>Passwort<input v-model="form.secret" type="password" :required="!form.id" autocomplete="new-password" :placeholder="form.id?'leer = behalten':''"></label></div>
  <label v-if="form.auth==='header'||form.auth==='query'">{{form.id?'Schlüssel ersetzen (leer = behalten)':form.header.toLowerCase()==='authorization'&&form.prefix==='Bearer '?'Bearer-Schlüssel der Anbieter-API':'API-Schlüssel der Anbieter-API'}}<input v-model="form.secret" type="password" :required="!form.id" minlength="8" autocomplete="new-password" placeholder="Wird nach dem Speichern nicht mehr angezeigt"></label>
  <label>Feste Zusatz-Header (optional)<textarea v-model="form.headersText" rows="2" placeholder="Notion-Version: 2022-06-28" spellcheck="false"></textarea><span class="hint">Einer pro Zeile als „Name: Wert“. Nicht geheim – Schlüssel gehören in das Feld oben.</span></label>
  <label v-if="form.auth!=='none'" class="checkbox"><input v-model="form.allow_private" type="checkbox">Internes Ziel ausdrücklich erlauben (z. B. Docker oder lokale Test-API)</label>
 </template>
 <template v-else-if="modal==='action'">
  <label>Name<input v-model="form.name" required placeholder="z. B. Kundenliste lesen"></label>
  <label>Verbindung<select v-model="form.connection_id" required :disabled="!!form.id"><option v-for="c in state.connections" :value="c.id">{{c.name}} · {{c.tenant}}</option></select></label>
  <label>Aktionsname für die Client-URL<input v-model="form.slug" required pattern="[a-z][a-z0-9-]{1,79}" placeholder="kunden-liste"></label>
  <div class="form-row"><label>Methode<select v-model="form.method"><option>GET</option><option>POST</option><option>PUT</option><option>PATCH</option><option>DELETE</option></select></label><label>Zielpfad<input v-model="form.path" required placeholder="/customers/{id}"></label></div>
  <p class="hint">{id} im Pfad füllt der Client mit ?id=…; feste Parameter direkt anhängen, z. B. /posts?status=publish.</p>
  <label>Freigegebene Query-Parameter (optional)<input v-model="form.queryText" placeholder="page, per_page, search"></label>
  <template v-if="methodsWithBody.includes(form.method)">
   <label>Body</label>
   <div class="radio-row"><label class="checkbox"><input v-model="form.bodyMode" type="radio" value="fields">Einfache Felder durchreichen</label><label class="checkbox"><input v-model="form.bodyMode" type="radio" value="template">Body-Vorlage</label></div>
   <label v-if="form.bodyMode==='fields'">Erlaubte JSON-Felder<input v-model="form.fieldsText" placeholder="name, email"></label>
   <label v-else>Body-Vorlage (JSON)<textarea v-model="form.body_template" rows="7" spellcheck="false" placeholder='[{"fields": {"Name": "{{name}}"}}]'></textarea><span class="hint">Der Client liefert nur die Werte der &#123;&#123;platzhalter&#125;&#125; als einfaches JSON-Objekt. Struktur, feste Werte und GraphQL-Abfragen bleiben in Schleuse.</span></label>
   <label>Format an die Ziel-API<select v-model="form.body_format"><option value="">JSON</option><option value="form">Formular (z. B. Stripe)</option></select></label>
  </template>
  <p class="muted">Neue Aktionen sind zunächst inaktiv. Danach testen, aktivieren und einen API-Token erstellen.</p>
 </template>
 <template v-else><label>Name<input v-model="form.name" required></label><div class="form-row"><label>Benutzername<input v-model="form.username" required pattern="[A-Za-z0-9][A-Za-z0-9_-]{1,49}" autocomplete="off"></label><label>Kunde<input v-model="form.tenant" required></label></div><label>Persönliches Passwort<input v-model="form.password" type="password" required minlength="12" autocomplete="new-password"></label><label>Erlaubte Aktionen</label><div class="grants"><label v-for="a in availableActions" class="checkbox"><input v-model="form.grants" :value="a.id" type="checkbox">{{a.name}}</label><p v-if="!availableActions.length" class="muted">Für diesen Kunden sind noch keine Aktionen angelegt.</p></div></template>
 <p v-if="error" class="alert error" role="alert">{{error}}</p><div class="modal-footer"><button type="button" class="secondary" @click="modal=''">Abbrechen</button><button class="primary" :disabled="busy">{{busy?'Speichert …':'Speichern'}}<Check :size="16"/></button></div></form></section></div>
</div>
</template>
