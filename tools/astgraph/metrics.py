import gzip, sys
import json, collections, re, sys
g = json.load(gzip.open(sys.argv[1]) if sys.argv[1].endswith('.gz') else open(sys.argv[1]))
M = g['module']; P = M + '/'
N = {n['id']: n for n in g['nodes']}
short = lambda s: s.replace(P, '').replace(M, '(root)')
kinds = collections.Counter(n['kind'] for n in g['nodes'])
ekinds = collections.Counter(e['kind'] for e in g['edges'])
print('NODES', dict(kinds)); print('EDGES', dict(ekinds))
pk = [n for n in g['nodes'] if n['kind']=='package']
imp = collections.defaultdict(set); ext = collections.defaultdict(set); fanin = collections.Counter()
for e in g['edges']:
    if e['kind']=='import': imp[e['from']].add(e['to']); fanin[e['to']]+=1
    if e['kind']=='ext_import': ext[e['from']].add(e['to'][4:])
def closure(p):
    seen={p}; st=[p]
    while st:
        for q in imp[st.pop()]:
            if q not in seen: seen.add(q); st.append(q)
    return seen
sig = ['go_stmts','selects','chan_ops','make_chan','locks','sleeps','atomics','sprintf','json']
agg = collections.defaultdict(collections.Counter)
for n in g['nodes']:
    if n['kind'] in ('func','method','type'):
        for s in sig: agg[n['pkg']][s]+=n.get(s,0)
rows=[]
for p in pk:
    c = closure(p['id']); loc = sum(N[x].get('lines',0) for x in c)
    e = set().union(*[ext[x] for x in c])
    key = sorted(m.split('/')[-1] if 'pebble' in m or 'kafka' in m or 'redis' in m or 'graphql' in m or 'prometheus' in m else '' for m in e)
    heavy = sorted({k for k in key if k})
    rows.append((short(p['id']), p.get('lines',0), loc, len(c)-1, fanin[p['id']], len(imp[p['id']]), ','.join(heavy)))
rows.sort(key=lambda r:-r[2])
print('\nPKG | own | closureLOC | closurePkgs | fanin | fanout | heavy-ext')
for r in rows: print(' | '.join(map(str,r)))
print('\nCONCURRENCY per pkg (go/select/chanops/makechan/locks/sleeps/atomics/sprintf/json)')
for p,c in sorted(agg.items(), key=lambda x:-x[1]['go_stmts']):
    if sum(c.values()): print(short(p), [c[s] for s in sig])
print('\nTOP FUNCS')
for n in sorted([n for n in g['nodes'] if n['kind'] in ('func','method')], key=lambda n:-n.get('lines',0))[:15]:
    print(n['lines'], short(n['id']), n.get('file'))
print('\nTOP TYPES by method set')
for n in sorted([n for n in g['nodes'] if n['kind']=='type'], key=lambda n:-n.get('methods',0))[:10]:
    print(n.get('methods'), short(n['id']))
print('\nINTERFACES per pkg')
ic = collections.Counter(n['pkg'] for n in g['nodes'] if n['kind']=='interface')
for p,c in ic.most_common(): print(c, short(p))
print('\nLARGEST INTERFACES')
for n in sorted([n for n in g['nodes'] if n['kind']=='interface'], key=lambda n:-n.get('methods',0))[:8]: print(n.get('methods'), short(n['id']))
print('\nCONCRETE ASSERTIONS')
for a in g['concrete_assertions']: print(a['at'], short(a['target']))
# KVStore usage outside storage
kv = collections.Counter(); kvsites=collections.Counter()
for e in g['edges']:
    if e['kind']=='iface_call' and re.match(re.escape(P)+r'pkg/storage\.KVStore\.', e['to']) and not e['from'].startswith(P+'pkg/storage'):
        kv[short(N.get(e['from'],{'pkg':e['from']})['pkg'])]+=e['n']; kvsites[short(e['from'])]+=e['n']
print('\nKVStore iface calls from outside storage', dict(kv), sum(kv.values()))
