#!/usr/bin/env python3
"""Lokaler Beispielclient: kennt nur den persönlichen Demo-Zugang, keinen Anbieter-Key."""
import json
from urllib.request import Request, build_opener, ProxyHandler
opener = build_opener(ProxyHandler({}))
base = 'http://127.0.0.1:18080'
login = Request(base+'/auth/login', data=json.dumps({'username':'tester','password':'Testzugang-2026!'}).encode(), headers={'Content-Type':'application/json'})
with opener.open(login, timeout=5) as response:
    token = json.load(response)['access_token']
request = Request(base+'/actions/kunden-liste', headers={'Authorization':'Bearer '+token})
with opener.open(request, timeout=20) as response:
    print('HTTP', response.status, '· Quelle:', response.headers.get('X-Gateway-Source'))
    print(json.dumps(json.load(response), ensure_ascii=False, indent=2))
