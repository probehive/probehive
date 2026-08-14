# Third-Party Notices

The authoritative dependency versions are recorded in `go.mod`, `go.sum`, and `web/package-lock.json`. The following Go modules are linked into the ProbeHive backend.

| Module | Version | License |
| --- | --- | --- |
| `github.com/jackc/pgx/v5` | `v5.10.0` | MIT |
| `github.com/jackc/puddle/v2` | `v2.2.2` | MIT |
| `github.com/jackc/pgpassfile` | `v1.0.0` | MIT |
| `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | MIT |
| `golang.org/x/crypto` | `v0.54.0` | BSD-3-Clause |
| `golang.org/x/sync` | `v0.22.0` | BSD-3-Clause |
| `golang.org/x/sys` | `v0.47.0` | BSD-3-Clause |
| `golang.org/x/text` | `v0.40.0` | BSD-3-Clause |
| `golang.org/x/time` | `v0.15.0` | BSD-3-Clause |

## Browser Runtime Packages

The following packages are compiled into the static browser application:

| Package | Version | License |
| --- | --- | --- |
| `@tanstack/query-core` | `5.101.4` | MIT |
| `@tanstack/react-query` | `5.101.4` | MIT |
| `cookie-es` | `3.1.1` | MIT |
| `react` | `19.2.8` | MIT |
| `react-dom` | `19.2.8` | MIT |
| `react-router` | `8.3.0` | MIT |
| `scheduler` | `0.27.0` | MIT |

Copyright notices from the exact package distributions:

- Copyright (c) 2021-present Tanner Linsley (`@tanstack/query-core`, `@tanstack/react-query`)
- Copyright (c) Meta Platforms, Inc. and affiliates (`react`, `react-dom`, `scheduler`)
- Copyright (c) React Training LLC 2015-2019 (`react-router`)
- Copyright (c) Remix Software Inc. 2020-2021 (`react-router`)
- Copyright (c) Shopify Inc. 2022-2023 (`react-router`)
- Cookie-es copyright (c) Pooya Parsa <pooya@pi0.io>
- Cookie parsing based on <https://github.com/jshttp/cookie>
- Copyright (c) 2012-2014 Roman Shtylman <shtylman@gmail.com> (`cookie-es`)
- Copyright (c) 2015 Douglas Christopher Wilson <doug@somethingdoug.com> (`cookie-es`)
- Set-Cookie parsing based on <https://github.com/nfriedly/set-cookie-parser>
- Copyright (c) 2015 Nathan Friedly <nathan@nfriedly.com> (<http://nfriedly.com/>) (`cookie-es`)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Container Images

The Compose package and its multi-stage builds use Docker Official Images pinned
to these multi-platform manifest digests:

| Image | Digest | Purpose | Primary upstream license |
| --- | --- | --- | --- |
| golang:1.26.5-alpine | sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 | build only | Go BSD-3-Clause; Alpine package licenses |
| node:24.18.0-bookworm | sha256:5711a0d445a1af54af9589066c646df387d1831a608226f4cd694fc59e745059 | build only | Node.js MIT; Debian package licenses |
| alpine:3.24 | sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b | API runtime | Alpine package licenses |
| nginx:1.31.1-alpine | sha256:8b1e78743a03dbb2c95171cc58639fef29abc8816598e27fb910ed2e621e589a | static web and same-origin gateway | nginx BSD-2-Clause; Alpine package licenses |
| postgres:17.10 | sha256:a426e44bac0b759c95894d68e1a0ac03ecc20b619f498a91aae373bf06d8508d | database runtime | PostgreSQL License; Debian package licenses |

The image filesystems retain their distribution package metadata and license
files. The pinned sources are maintained at docker-library/golang,
nodejs/docker-node, alpinelinux/docker-alpine, nginx/docker-nginx, and
docker-library/postgres.

## MIT-Licensed Jack Christensen Modules

Copyright notices from the exact module distributions:

- Copyright (c) 2013-2021 Jack Christensen (`pgx`)
- Copyright (c) 2018 Jack Christensen (`puddle`)
- Copyright (c) 2019 Jack Christensen (`pgpassfile`)
- Copyright (c) 2020 Jack Christensen (`pgservicefile`)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## Go Authors BSD-3-Clause Modules

Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:

- Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.
- Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer in the documentation and/or other materials provided with the distribution.
- Neither the name of Google LLC nor the names of its contributors may be used to endorse or promote products derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

Before adding a dependency or asset, review its ownership, provenance, maintenance, security advisories, transitive dependencies, supported platforms, and exact-version license. Preserve any additional attribution or redistribution terms here.
