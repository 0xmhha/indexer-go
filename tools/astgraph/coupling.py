import gzip, sys
import json, collections
g = json.load(gzip.open(sys.argv[1]) if sys.argv[1].endswith('.gz') else open(sys.argv[1])); M=g['module']; P=M+'/'
N={n['id']:n for n in g['nodes']}
s=lambda x:x.replace(P,'')
# storage interfaces referenced from outside pkg/storage, by consumer package
use=collections.defaultdict(set); conc=collections.defaultdict(set)
for e in g['edges']:
    if e['kind']!='ref' or not e['to'].startswith(P+'pkg/storage.'): continue
    src=N.get(e['from']); 
    if not src: continue
    sp=src['pkg']
    if sp.startswith(P+'pkg/storage'): continue
    t=N.get(e['to'])
    if not t: continue
    if t['kind']=='interface': use[s(sp)].add(t['name'])
    elif t['kind']=='type' and t['name'] in ('PebbleStorage','GenesisInitializingStorage','Config'): conc[s(sp)].add(t['name'])
print('STORAGE INTERFACE CONSUMERS')
for p in sorted(use): print(p, '|', ', '.join(sorted(use[p])), '| concrete:', ', '.join(sorted(conc[p])) or '-')
# mermaid package import graph (internal only, excluding e2e, testutil)
print('\nMERMAID')
print('graph TD')
for e in g['edges']:
    if e['kind']=='import' and not s(e['from']).startswith('e2e'):
        a,b=s(e['from']),s(e['to'])
        print(f'  {a.replace("/","_")}["{a}"] --> {b.replace("/","_")}["{b}"]')
# declaration closure for seeds
adj=collections.defaultdict(set)
for e in g['edges']:
    if e['kind'] in ('call','ref','iface_call'): adj[e['from']].add(e['to'])
for n in g['nodes']:  # type -> its methods
    if n['kind']=='method':
        t=n['id'].split('.(')[0]+'.'+n['id'].split('.(')[1].split(')')[0]; adj[t].add(n['id'])
    if n['kind']=='iface_method':
        pass
def clo(seeds):
    seen=set(seeds); st=list(seeds)
    while st:
        for q in adj[st.pop()]:
            if q not in seen and q in N: seen.add(q); st.append(q)
    d=[N[x] for x in seen if N[x]['kind'] in ('func','method','type','interface')]
    pk=collections.Counter(s(x['pkg']) for x in d)
    return len(d), sum(x.get('lines',0) for x in d), pk.most_common(6)
print('\nDECL CLOSURES')
for seed in ['pkg/fetch.(Fetcher).FillGaps','pkg/fetch.(Fetcher).Run','pkg/api/middleware.RateLimit','pkg/abi.NewDecoder','pkg/eventbus.NewLocalEventBus','pkg/events.NewEventBus','pkg/fetch.NewFetcher','pkg/adapters/factory.NewFactory','pkg/token.NewDetector']:
    if P+seed in N: print(seed, clo([P+seed]))
    else: print(seed,'(missing)')
