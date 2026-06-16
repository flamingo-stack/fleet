#!/usr/bin/env python3
"""
Generate an OpenAPI 3.0 spec for the Fleet REST API by parsing the route table
in server/service/handler.go.

This produces COMPLETE path/method/auth/param coverage of every registered HTTP
endpoint. Request/response *bodies* are intentionally light: write endpoints get
a permissive JSON body referencing the Go request type (see each operation's
description), and list endpoints get Fleet's standard pagination query params.
For fully-specified request/response schemas of the fork's OpenFrame endpoints,
see fleet-openframe-openapi.yaml.

Re-run after an upstream sync:  python3 openframe/scripts/gen_openapi.py
"""
import re
import os
from collections import OrderedDict

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
HANDLER = os.path.join(ROOT, "server", "service", "handler.go")
OUT = os.path.join(ROOT, "openframe", "docs", "fleet-openapi.yaml")
STRUCT_DIRS = [
    os.path.join(ROOT, "server", "service"),
    os.path.join(ROOT, "ee", "server", "service"),
]

# Endpoint registrar variable -> (security scheme name or None, note)
REGISTRAR_SECURITY = {
    "ue": ("bearerAuth", None),
    "de": ("deviceAuthToken", "Authenticated by the device-specific token in the URL path."),
    "he": ("osqueryNodeKey", "Authenticated by the osquery node key sent in the request body."),
    "oe": ("orbitNodeKey", "Authenticated by the Orbit node key sent in the request body."),
    "neOrbit": (None, "No authentication (Orbit enroll/no-auth endpoint)."),
    "ne": (None, "No authentication required."),
    "androidEndpoints": ("bearerAuth", "Android management endpoint."),
}

LIST_OPTION_PARAMS = [
    ("page", "integer", "Page number of the results to fetch (0-indexed)."),
    ("per_page", "integer", "Results per page. 0 means unlimited."),
    ("order_key", "string", "Column to order the results by."),
    ("order_direction", "string", "Order direction: 'asc' or 'desc'. Requires order_key."),
    ("query", "string", "Search query string matched against the entity's columns."),
    ("after", "string", "Cursor: the value to start results after (used with order_key)."),
]

METHOD_CALL_RE = re.compile(r'\.(GET|POST|PUT|PATCH|DELETE|HEAD)\(')
START_VER_RE = re.compile(r'StartingAtVersion\(\s*"([^"]+)"\s*\)')
PATHPARAM_RE = re.compile(r'\{([^}:]+)(?::([^}]+))?\}')


def split_top_commas(args):
    """Split an arg string on top-level commas (honoring quotes and nested brackets)."""
    parts, cur, depth, instr, esc = [], "", 0, False, False
    for c in args:
        if esc:
            cur += c
            esc = False
            continue
        if instr:
            cur += c
            if c == "\\":
                esc = True
            elif c == '"':
                instr = False
            continue
        if c == '"':
            instr = True
            cur += c
        elif c in "([{":
            depth += 1
            cur += c
        elif c in ")]}":
            depth -= 1
            cur += c
        elif c == "," and depth == 0:
            parts.append(cur)
            cur = ""
        else:
            cur += c
    if cur.strip():
        parts.append(cur)
    return [p.strip() for p in parts]


def extract_routes(text):
    """Yield (registrar, prefix, method, path, endpoint_fn, req_type) for every route call,
    handling multi-line args, dotted request types, and function-call endpoints."""
    for m in METHOD_CALL_RE.finditer(text):
        method = m.group(1)
        open_idx = m.end() - 1  # index of '('
        depth, j = 0, open_idx
        while j < len(text):
            c = text[j]
            if c == "(":
                depth += 1
            elif c == ")":
                depth -= 1
                if depth == 0:
                    break
            j += 1
        args = text[open_idx + 1:j]
        line_start = text.rfind("\n", 0, m.start()) + 1
        prefix = text[line_start:m.start()]
        rm = re.match(r'\s*([A-Za-z_]\w*)', prefix)
        if not rm:
            continue
        registrar = rm.group(1)
        parts = split_top_commas(args)
        if not parts or not (parts[0].startswith('"') and parts[0].endswith('"')):
            continue
        path = parts[0][1:-1]
        if not path.startswith("/"):
            continue
        fn_expr = parts[1] if len(parts) > 1 else ""
        fn_m = re.match(r'&?([A-Za-z_][\w.]*)', fn_expr)
        endpoint_fn = fn_m.group(1).split(".")[-1] if fn_m else "handler"
        req_expr = parts[2] if len(parts) > 2 else "nil"
        if req_expr == "nil":
            req_type = "nil"
        else:
            rt = re.match(r'&?([A-Za-z_][\w.]*)', req_expr)
            req_type = (rt.group(1).split(".")[-1] if rt else "nil")
        yield registrar, prefix, method, path, endpoint_fn, req_type


def load_structs():
    """structName -> raw body text between 'type X struct {' and the closing '}'."""
    structs = {}
    for d in STRUCT_DIRS:
        if not os.path.isdir(d):
            continue
        for fn in os.listdir(d):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            with open(os.path.join(d, fn), encoding="utf-8") as f:
                src = f.read()
            for m in re.finditer(r'type\s+(\w+)\s+struct\s*\{(.*?)\n\}', src, re.DOTALL):
                structs.setdefault(m.group(1), m.group(2))
    return structs


def go_type_to_openapi(go_type):
    t = go_type.lstrip("*[]")
    if t in ("uint", "int", "int64", "uint64", "uint32", "int32", "uint8"):
        return "integer"
    if t in ("bool",):
        return "boolean"
    if t in ("float64", "float32"):
        return "number"
    return "string"


def struct_query_params(struct_name, structs, _seen=None):
    """Extract query params: explicit query:"..." tags + embedded ListOptions."""
    if _seen is None:
        _seen = set()
    if struct_name in _seen:
        return []
    _seen.add(struct_name)
    body = structs.get(struct_name)
    params = []
    if body is None:
        return params
    if "ListOptions" in body:
        for name, typ, desc in LIST_OPTION_PARAMS:
            params.append((name, typ, desc, False))
    for line in body.splitlines():
        qm = re.search(r'query:"([^",]+)([^"]*)"', line)
        if qm:
            name = qm.group(1)
            optional = "optional" in qm.group(2)
            tm = re.match(r'\s*\w+\s+([\w\.\*\[\]]+)', line)
            typ = go_type_to_openapi(tm.group(1)) if tm else "string"
            params.append((name, typ, "", optional))
    # de-dup by name, preserve order
    out, seen_names = [], set()
    for p in params:
        if p[0] in seen_names:
            continue
        seen_names.add(p[0])
        out.append(p)
    return out


def humanize(endpoint_fn):
    name = re.sub(r'Endpoint$', '', endpoint_fn)
    words = re.sub(r'([a-z0-9])([A-Z])', r'\1 \2', name)
    words = words.replace("_", " ")
    return (words[:1].upper() + words[1:]).strip() or endpoint_fn


def tag_for(path):
    segs = [s for s in path.strip("/").split("/") if s]
    # drop 'api' and the version segment
    segs = [s for s in segs if s != "api"]
    if segs and segs[0] in ("v1", "2022-04", "latest", "_version_"):
        segs = segs[1:]
    if not segs:
        return "misc"
    if segs[0] == "fleet":
        segs = segs[1:]
    if not segs:
        return "fleet"
    seg = segs[0]
    if seg.startswith("{"):
        return "misc"
    return seg


def main():
    with open(HANDLER, encoding="utf-8") as f:
        text = f.read()
    structs = load_structs()

    # path -> method -> operation
    paths = OrderedDict()
    used_op_ids = {}
    security_schemes_used = set()
    tags_used = OrderedDict()
    count = 0
    dup = 0

    for registrar, prefix, method, raw_path, endpoint_fn, req_type in extract_routes(text):
        if registrar not in REGISTRAR_SECURITY:
            continue  # skip non-route helpers

        # resolve version segment
        version = "v1"
        sv = START_VER_RE.search(prefix)
        if sv and sv.group(1) != "v1":
            version = sv.group(1)
        path = raw_path.replace("_version_", version)

        # path params
        params = []
        for pm in PATHPARAM_RE.finditer(raw_path):
            pname, pregex = pm.group(1), pm.group(2)
            ptype = "integer" if (pregex and "0-9" in pregex) else "string"
            params.append(OrderedDict([
                ("name", pname),
                ("in", "path"),
                ("required", True),
                ("schema", OrderedDict([("type", ptype)])),
            ]))
        # normalize path params in the path string ({id:[0-9]+} -> {id})
        clean_path = PATHPARAM_RE.sub(lambda m: "{%s}" % m.group(1), path)

        # query params from the request struct
        if req_type != "nil":
            for qname, qtype, qdesc, optional in struct_query_params(req_type, structs):
                if any(p["name"] == qname for p in params):
                    continue
                qp = OrderedDict([
                    ("name", qname),
                    ("in", "query"),
                    ("required", False),
                    ("schema", OrderedDict([("type", qtype)])),
                ])
                if qdesc:
                    qp["description"] = qdesc
                params.append(qp)

        sec_scheme, reg_note = REGISTRAR_SECURITY[registrar]
        tag = tag_for(path)
        tags_used.setdefault(tag, True)

        # operationId (unique)
        op_id = endpoint_fn
        if op_id in used_op_ids:
            used_op_ids[op_id] += 1
            op_id = "%s_%d" % (endpoint_fn, used_op_ids[endpoint_fn])
        else:
            used_op_ids[endpoint_fn] = 1

        desc_bits = []
        if req_type != "nil":
            desc_bits.append("Request mapped from Go type `%s` (server/service)." % req_type)
        if reg_note:
            desc_bits.append(reg_note)

        op = OrderedDict()
        op["operationId"] = op_id
        op["summary"] = humanize(endpoint_fn)
        op["tags"] = [tag]
        if desc_bits:
            op["description"] = " ".join(desc_bits)
        if params:
            op["parameters"] = params
        if method in ("POST", "PUT", "PATCH", "DELETE") and req_type != "nil":
            op["requestBody"] = OrderedDict([
                ("required", False),
                ("content", OrderedDict([
                    ("application/json", OrderedDict([
                        ("schema", OrderedDict([
                            ("type", "object"),
                            ("additionalProperties", True),
                            ("description",
                             "Body fields map from Go type `%s`; not enumerated in this generated "
                             "spec. See docs/REST API/rest-api.md." % req_type),
                        ])),
                    ])),
                ])),
            ])
        # responses
        responses = OrderedDict()
        responses["200"] = OrderedDict([
            ("description", "Success"),
            ("content", OrderedDict([
                ("application/json", OrderedDict([
                    ("schema", OrderedDict([("type", "object")])),
                ])),
            ])),
        ])
        for code, dsc in (("400", "Bad request"), ("401", "Unauthorized"),
                          ("403", "Forbidden"), ("404", "Not found"),
                          ("500", "Internal server error")):
            responses[code] = OrderedDict([
                ("description", dsc),
                ("content", OrderedDict([
                    ("application/json", OrderedDict([
                        ("schema", OrderedDict([("$ref", "#/components/schemas/Error")])),
                    ])),
                ])),
            ])
        op["responses"] = responses
        if sec_scheme:
            op["security"] = [OrderedDict([(sec_scheme, [])])]
            security_schemes_used.add(sec_scheme)
        else:
            op["security"] = []

        path_item = paths.setdefault(clean_path, OrderedDict())
        ml = method.lower()
        if ml in path_item:
            dup += 1
            continue
        path_item[ml] = op
        count += 1

    spec = build_spec(paths, tags_used, security_schemes_used)
    with open(OUT, "w", encoding="utf-8") as f:
        f.write("\n".join(emit(spec, 0)) + "\n")
    print("Wrote %s: %d operations, %d paths (%d duplicate method+path skipped)"
          % (OUT, count, len(paths), dup))


def build_spec(paths, tags_used, security_schemes_used):
    comp_schemes = OrderedDict()
    if "bearerAuth" in security_schemes_used:
        comp_schemes["bearerAuth"] = OrderedDict([
            ("type", "http"), ("scheme", "bearer"),
            ("description", "Fleet session / API-only user token: `Authorization: Bearer <token>`."),
        ])
    for name, where, desc in [
        ("deviceAuthToken", "path", "Per-device token embedded in the URL path ({token})."),
        ("osqueryNodeKey", "body", "osquery node key sent in the JSON request body as `node_key`."),
        ("orbitNodeKey", "body", "Orbit node key sent in the JSON request body as `orbit_node_key`."),
    ]:
        if name in security_schemes_used:
            comp_schemes[name] = OrderedDict([
                ("type", "apiKey"), ("in", "header"), ("name", "X-" + name),
                ("description", desc + " (Represented as apiKey for tooling; actual transport is "
                 + where + ".)"),
            ])

    error_schema = OrderedDict([
        ("type", "object"),
        ("properties", OrderedDict([
            ("message", OrderedDict([("type", "string")])),
            ("errors", OrderedDict([
                ("type", "array"),
                ("items", OrderedDict([
                    ("type", "object"),
                    ("properties", OrderedDict([
                        ("name", OrderedDict([("type", "string")])),
                        ("reason", OrderedDict([("type", "string")])),
                    ])),
                ])),
            ])),
        ])),
    ])

    spec = OrderedDict()
    spec["openapi"] = "3.0.3"
    spec["info"] = OrderedDict([
        ("title", "Fleet REST API (OpenFrame fork)"),
        ("version", "generated"),
        ("description",
         "Auto-generated from server/service/handler.go by openframe/scripts/gen_openapi.py. "
         "COMPLETE coverage of every registered endpoint (path, method, auth, path/query params). "
         "Request/response bodies are intentionally light here — see fleet-openframe-openapi.yaml "
         "for fully-specified schemas of the fork's OpenFrame host-assignment endpoints, and "
         "'docs/REST API/rest-api.md' for upstream Fleet's full reference. "
         "The {version} path segment accepts 'v1', '2022-04', or 'latest'. "
         "List endpoints accept the standard pagination query params (page, per_page, order_key, "
         "order_direction, query, after)."),
    ])
    spec["servers"] = [OrderedDict([
        ("url", "https://{host}"),
        ("variables", OrderedDict([
            ("host", OrderedDict([("default", "fleet.example.com")])),
        ])),
    ])]
    spec["tags"] = [OrderedDict([("name", t)]) for t in sorted(tags_used)]
    spec["paths"] = paths
    spec["components"] = OrderedDict([
        ("securitySchemes", comp_schemes),
        ("schemas", OrderedDict([("Error", error_schema)])),
    ])
    return spec


# ---- minimal block-style YAML emitter (dict/list/str/int/bool/None) ----
def _scalar(v):
    if v is None:
        return "null"
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (int, float)):
        return str(v)
    s = str(v)
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def emit(obj, indent):
    pad = "  " * indent
    out = []
    if isinstance(obj, dict):
        if not obj:
            return [pad + "{}"]
        for k, v in obj.items():
            key = _scalar(str(k))
            if isinstance(v, (dict, list)) and v:
                out.append("%s%s:" % (pad, key))
                out.extend(emit(v, indent + 1))
            elif isinstance(v, dict):
                out.append("%s%s: {}" % (pad, key))
            elif isinstance(v, list):
                out.append("%s%s: []" % (pad, key))
            else:
                out.append("%s%s: %s" % (pad, key, _scalar(v)))
    elif isinstance(obj, list):
        for item in obj:
            if isinstance(item, (dict, list)) and item:
                sub = emit(item, indent + 1)
                # convert first line's leading pad into a '- ' marker
                first = sub[0]
                sub[0] = pad + "- " + first[len(pad) + 2:]
                out.extend(sub)
            else:
                out.append("%s- %s" % (pad, _scalar(item)))
    return out


if __name__ == "__main__":
    main()
