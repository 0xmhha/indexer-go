#!/usr/bin/env python3
"""Query the astgraph code graph of indexer-go (production code, no tests).

Generate the graph first:  (cd tools/astgraph && go run . -dir ../.. -out out)
then:                      python3 tools/astgraph/query.py <cmd> <arg>
The graph file is $ASTGRAPH (default tools/astgraph/out/graph.json).

usage:
  query.py find <substring>            nodes whose id contains substring
  query.py reach <substring>           is each matching node reachable from production roots?
  query.py callers <substring>         who calls / references the matching nodes
  query.py callees <substring>         what the matching nodes call / reference
  query.py path <substring>            one call path from a root to the first matching node
  query.py dead <pkg-substring>        exported funcs/methods/types of packages not reachable
  query.py impls <interface-substring> types implementing the interface

Roots: github.com/0xmhha/indexer-go/cmd/indexer.main and every package's init
(registrations run at link time). Reachability follows call, iface_call and ref
edges. A method is reached by a direct call or reference, or when the interface
method it implements is called on a reachable path. Methods called only through
reflection (graphql-go resolvers) or standard-library interfaces (http.Handler,
io.Closer, json.Marshaler) show as unreached although they run.
"""
import collections, json, os, sys

G = json.load(open(os.environ.get("ASTGRAPH") or os.path.join(os.path.dirname(os.path.abspath(__file__)), "out", "graph.json")))
M = G["module"] + "/"
N = {n["id"]: n for n in G["nodes"]}
out = collections.defaultdict(set)
inn = collections.defaultdict(set)
for e in G["edges"]:
    if e["kind"] in ("call", "iface_call", "ref", "contains", "implements"):
        out[e["from"]].add((e["to"], e["kind"]))
        inn[e["to"]].add((e["from"], e["kind"]))

def short(i): return i.replace(M, "")

# methods by receiver type id: method ids look like pkg.(*T).M or pkg.T.M
methods = collections.defaultdict(list)
for n in G["nodes"]:
    if n["kind"] == "method":
        name = n["id"].rsplit(".", 1)[0].replace("(*", "").replace("(", "").replace(")", "")
        methods[name].append(n["id"])

roots = [i for i, n in N.items() if n["kind"] == "func" and (n["name"] == "init" or i == M + "cmd/indexer.main")]
# init funcs may be numbered (init#1): include any func named init*
roots += [i for i, n in N.items() if n["kind"] == "func" and n["name"].startswith("init#")]

# implementers[interface id] = types implementing it
implementers = collections.defaultdict(set)
for e in G["edges"]:
    if e["kind"] == "implements":
        implementers[e["to"]].add(e["from"])

def reachable():
    """Nodes reachable from the roots. A method is reached by a direct call
    or reference, or when the interface method it implements is called on
    a reachable path (iface_call)."""
    seen, prev = set(roots), {}
    st = list(roots)
    while st:
        x = st.pop()
        nxt = [t for t, k in out[x] if k not in ("implements", "contains")]
        n = N.get(x)
        if n and n["kind"] == "iface_method":
            iface, name = x.rsplit(".", 1)
            for t in implementers.get(iface, ()):
                for m in methods.get(t, []):
                    if m.rsplit(".", 1)[1] == name:
                        nxt.append(m)
        for t in nxt:
            if t not in seen:
                seen.add(t); prev[t] = x; st.append(t)
    return seen, prev

def match(s): return sorted(i for i in N if s in i)

cmd, arg = sys.argv[1], (sys.argv[2] if len(sys.argv) > 2 else "")
if cmd == "find":
    for i in match(arg)[:200]:
        n = N[i]; print(n["kind"], short(i), f'{short(n.get("file",""))}:{n.get("line","")}')
elif cmd == "reach":
    R, _ = reachable()
    for i in match(arg)[:200]:
        print("REACHABLE  " if i in R else "UNREACHED  ", N[i]["kind"], short(i))
elif cmd in ("callers", "callees"):
    for i in match(arg)[:50]:
        print("==", short(i))
        rel = inn[i] if cmd == "callers" else out[i]
        for t, k in sorted(rel)[:80]:
            print("  ", k, short(t))
elif cmd == "path":
    R, prev = reachable()
    for i in match(arg):
        if i in R:
            p = [i]
            while p[-1] in prev: p.append(prev[p[-1]])
            print(" <- ".join(short(x) for x in p)); break
    else:
        print("no matching node is reachable")
elif cmd == "dead":
    R, _ = reachable()
    for i, n in sorted(N.items()):
        if arg in n["pkg"] and n["kind"] in ("func", "method", "type", "interface") and n.get("exported") and i not in R:
            print(n["kind"], short(i), f'{short(n.get("file",""))}:{n.get("line","")}')
elif cmd == "impls":
    for i in match(arg):
        if N[i]["kind"] != "interface": continue
        print("==", short(i))
        for t, k in sorted(inn[i]):
            if k == "implements": print("  ", short(t))
else:
    print(__doc__)
