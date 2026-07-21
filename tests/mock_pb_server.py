#!/usr/bin/env python3
"""Mock PB + Backend server for tool tests.
Writes state log to STATE_FILE for assertions."""
import http.server, json, os, sys, threading, time, urllib.parse

STATE_FILE = os.environ.get('STATE_FILE', '/tmp/mock_pb_state.json')

state = {
    'records': {
        'contracts/q9wynhz1pi4tpvh': {
            'id': 'q9wynhz1pi4tpvh', 'brutto_price': 1000, 'netto_price': 123,
            'tour_amount_currency': 'USD', 'finance_status': 'approved',
            'is_cancelled': False, 'is_deleted': False, 'is_rejected': False,
            'office': 'h00d7lg15350gz8', 'tour_operator': 'Великолепный Век', 'notes': []
        },
        'applications/sgj6hnms9bmg7yi': {
            'id': 'sgj6hnms9bmg7yi', 'contract_id': 'q9wynhz1pi4tpvh',
            'provider_id': '28u79qpw6gjce96', 'number': '100221',
            'amount': 123, 'currency': 'USD', 'status': 'active',
            'finance_status': 'approved', 'is_primary': True, 'is_deleted': False,
            'expand': {'provider_id': {'id': '28u79qpw6gjce96', 'name': 'ANEX', 'is_active': True}}
        },
        'providers/28u79qpw6gjce96': {'id': '28u79qpw6gjce96', 'name': 'ANEX', 'is_active': True},
        'providers/24eq9ce8yc53w9w': {'id': '24eq9ce8yc53w9w', 'name': 'KOMPAS', 'is_active': True},
        'payment_methods/on3y22ok00pb60j': {'id': 'on3y22ok00pb60j', 'name': 'Наличные USD', 'short_name': 'Наличные USD', 'is_active': True},
        'payment_methods/etyluafospvweo7': {'id': 'etyluafospvweo7', 'name': 'Наличные KGS', 'short_name': 'Наличные KGS', 'is_active': True},
    },
    'created': {},
    'log': [],
}


def save_state():
    with open(STATE_FILE, 'w') as f:
        json.dump(state, f)



class PBHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        if self.path.endswith('/auth-with-password'):
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({'token': 'mocktoken'}).encode())
            return
        parts = self.path.strip('/').split('/')
        col = parts[2] if len(parts) > 2 else ''
        data = json.loads(body) if body else {}
        rid = 'rec' + str(len(state['created']) + 1)
        data['id'] = rid
        state['created'][rid] = {'col': col, 'data': data}
        state['log'].append({'method': 'POST', 'collection': col, 'id': rid, 'body': data})
        save_state()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def do_PATCH(self):
        body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        parts = self.path.strip('/').split('/')
        col = parts[2] if len(parts) > 2 else ''
        rid = parts[4].split('?')[0] if len(parts) > 4 else ''
        data = json.loads(body) if body else {}
        key = f'{col}/{rid}'
        if key in state['records']:
            state['records'][key].update(data)
            state['log'].append({'method': 'PATCH', 'collection': col, 'id': rid, 'body': data})
            save_state()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps(state['records'][key]).encode())
        elif rid in state['created']:
            state['created'][rid]['data'].update(data)
            state['log'].append({'method': 'PATCH', 'collection': col, 'id': rid, 'body': data})
            save_state()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps(state['created'][rid]['data']).encode())
        else:
            self.send_response(404)
            self.end_headers()

    def do_GET(self):
        path_parts = self.path.strip('/').split('?')[0].strip('/').split('/')
        col = path_parts[2] if len(path_parts) > 2 else ''
        if len(path_parts) > 4 and path_parts[3] == 'records':
            rid = path_parts[4]
            key = f'{col}/{rid}'
            rec = state['records'].get(key)
            if rec is None:
                created = state['created'].get(rid)
                rec = created['data'] if created else None
            if rec is not None:
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps(rec).encode())
                return
            self.send_response(404)
            self.end_headers()
            return
        # List
        items = [v for k, v in state['records'].items() if k.startswith(col + '/')]
        items += [c['data'] for c in state['created'].values() if c['col'] == col]
        if col == 'applications' and 'contract_id' in self.path:
            qs = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
            filt = qs.get('filter', [''])[0]
            # Parse filter: contract_id="x" && provider_id="y" && number="z" && is_deleted!=true
            import re
            # Quoted equality: field="value"
            for m in re.finditer(r'(\w+)\s*=\s*"([^"]*)"', filt):
                fld, val = m.group(1), m.group(2)
                items = [i for i in items if str(i.get(fld)) == val]
            # Unquoted inequality: is_deleted!=true (exclude deleted)
            if re.search(r'is_deleted\s*!=\s*true', filt):
                items = [i for i in items if not i.get('is_deleted')]
        if col == 'providers' or col == 'payment_methods':
            items = list(state['records'].values()) + [c['data'] for c in state['created'].values()]
        if col == 'application_corrections' or col == 'operator_payment_requests':
            items = [c['data'] for c in state['created'].values() if c['col'] == col]
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps({'items': items, 'totalItems': len(items)}).encode())


class BackendHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.path == '/rates':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({
                'usd': {'buy': 87.3, 'sell': 87.8},
                'eur': {'buy': 99.7, 'sell': 100.7},
                'ts': 1234567890
            }).encode())
        else:
            self.send_response(404)
            self.end_headers()


if __name__ == '__main__':
    pb = http.server.HTTPServer(('127.0.0.1', 18090), PBHandler)
    be = http.server.HTTPServer(('127.0.0.1', 18091), BackendHandler)
    threading.Thread(target=pb.serve_forever, daemon=True).start()
    threading.Thread(target=be.serve_forever, daemon=True).start()
    save_state()
    print(f"Mock PB on :18090, Backend on :18091, state: {STATE_FILE}", flush=True)
    while True:
        time.sleep(60)
