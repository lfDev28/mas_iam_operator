#!/usr/bin/env python3
"""Headless MAS logins through the mas-est Keycloak (SAML and OIDC).

  scripts/mas-login-check.py --home https://home.<instance>.<apps-domain>
  scripts/mas-login-check.py --home https://home.... saml.user2 oidc.user1
  make login-check HOME_URL=https://home....

Drives the same redirects and form posts a browser does: MAS home -> coreidp
-> Keycloak login form -> back through coreidp's SAML ACS or OIDC redirect.
A user passes when coreidp issues a MAS authorization code to
home/.../auth/callback, i.e. MAS accepted Keycloak's assertion or token and
authorized the user. Stops there: no MAS session is created.

Users default to the Keycloak demo users (oidc.user1/2, saml.user1/2,
scim.user1/2). The provider is taken from the name: saml.* uses SAML,
everything else OIDC. Password: --password, else $MAS_EST_DEMO_USER_PASSWORD,
else the demo default. LDAP logins go through the MAS login page and are not
covered.

Note: a first login of a user not yet in MAS self-registers it, as in a browser.
Exit status: 0 if every user passed, 1 otherwise. Python 3 stdlib only.
"""
import argparse
import html
import html.parser
import http.cookiejar
import os
import re
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request

DEMO_USERS = ["oidc.user1", "oidc.user2", "saml.user1", "saml.user2", "scim.user1", "scim.user2"]

# coreidp error codes seen on /login?error_description=..., with what they mean.
MAS_ERRORS = {
    "AIUOM0013E": "user is not in MAS and self-registration did not create it",
    "AIUOM0024E": "user is deactivated in MAS",
}


class Forms(html.parser.HTMLParser):
    def __init__(self):
        super().__init__()
        self.forms, self._cur = [], None

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if tag == "form":
            self._cur = {"id": a.get("id", ""), "action": html.unescape(a.get("action") or ""),
                         "method": (a.get("method") or "get").upper(), "fields": {}}
            self.forms.append(self._cur)
        elif tag == "input" and self._cur is not None and a.get("name"):
            self._cur["fields"][a["name"]] = a.get("value") or ""

    def handle_endtag(self, tag):
        if tag == "form":
            self._cur = None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def cookie(name, value, host):
    return http.cookiejar.Cookie(0, name, value, None, False, host, False, False, "/", True,
                                 True, None, False, None, None, {})


def login(home, user, password, provider, max_hops=30):
    """Returns (ok, message, hops)."""
    ctx = ssl.create_default_context()
    ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE  # demo clusters: self-signed routes
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx),
                                         urllib.request.HTTPCookieProcessor(jar), NoRedirect)
    home_host = urllib.parse.urlparse(home).hostname
    auth_host = "auth." + home_host.split(".", 1)[1]
    # The MAS login page sets this cookie after the user picks a provider;
    # coreidp's Liberty authFilters route on it.
    name, value = ("mas-saml", "saml") if provider == "saml" else ("mas-oidc", "oidc")
    jar.set_cookie(cookie(name, value, auth_host))

    hops, url, data, sent_password = [], home.rstrip("/") + "/", None, False
    for _ in range(max_hops):
        req = urllib.request.Request(url, data=urllib.parse.urlencode(data).encode() if data is not None else None)
        try:
            resp = opener.open(req, timeout=30)
            code, location, body = resp.status, None, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            code, location, body = e.code, e.headers.get("Location"), e.read().decode("utf-8", "replace")
        except (urllib.error.URLError, OSError) as e:
            return False, f"cannot reach {urllib.parse.urlparse(url).hostname}: {e}", hops
        hops.append(f"{'POST' if data is not None else 'GET ':4} {code} {url[:140]}")
        data = None

        if location:
            url = urllib.parse.urljoin(url, location)
            parsed = urllib.parse.urlparse(url)
            if parsed.hostname == home_host and "/auth/callback" in parsed.path:
                if "code" in urllib.parse.parse_qs(parsed.query):
                    return True, "MAS issued an authorization code", hops
                return False, f"MAS callback without a code: {url[:200]}", hops
            continue
        if "error_description=" in url:
            err = urllib.parse.parse_qs(urllib.parse.urlparse(url).query).get("error_description", ["?"])[0]
            return False, f"MAS refused the login: {err} ({MAS_ERRORS.get(err, 'see the coreidp log')})", hops
        if code != 200:
            return False, f"HTTP {code} from {url[:160]}", hops

        # Liberty's script hops: store the current URL in a cookie, then location.replace().
        for cname, encoded in re.findall(
                r"""document\.cookie\s*=\s*['"]([A-Za-z0-9_]+)=['"]\s*\+\s*(encodeURIComponent\()?""", body):
            value = urllib.parse.quote(url, safe="") if encoded else url
            jar.set_cookie(cookie(cname, value, urllib.parse.urlparse(url).hostname))
        forms = Forms()
        forms.feed(body)
        replace = re.search(r"""window\.location\.replace\(["']([^"']+)["']\)""", body)
        if replace and not forms.forms:
            url = urllib.parse.urljoin(url, html.unescape(replace.group(1)))
            continue

        kc_login = next((f for f in forms.forms if f["id"] == "kc-form-login"), None)
        if kc_login:
            if sent_password:
                return False, "Keycloak rejected the username or password", hops
            sent_password = True
            data = dict(kc_login["fields"], username=user, password=password)
            url = urllib.parse.urljoin(url, kc_login["action"])
            continue
        auto_post = next((f for f in forms.forms if f["method"] == "POST" and f["action"]), None)
        if auto_post:  # SAMLRequest / SAMLResponse hand-offs
            data, url = auto_post["fields"], urllib.parse.urljoin(url, auto_post["action"])
            continue
        text = " ".join(re.sub(r"<[^>]+>", " ", body).split())
        return False, f"stuck at {url[:160]}: {text[:300]}", hops
    return False, f"gave up after {max_hops} hops", hops


def main():
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("--home", required=True, help="MAS home URL, https://home.<instance>.<apps-domain>")
    p.add_argument("--password", default=os.environ.get("MAS_EST_DEMO_USER_PASSWORD", "maxadmin"))
    p.add_argument("-v", "--verbose", action="store_true", help="print the redirect chain for every user")
    p.add_argument("users", nargs="*", default=DEMO_USERS)
    o = p.parse_args()
    if not o.home.startswith("https://home."):
        p.error("--home must look like https://home.<instance>.<apps-domain>")

    failed = 0
    for user in o.users:
        provider = "saml" if user.startswith("saml.") else "oidc"
        ok, message, hops = login(o.home, user, o.password, provider)
        print(f"{'PASS' if ok else 'FAIL'}  {provider:4}  {user:12}  {message}")
        if not ok or o.verbose:
            for h in hops:
                print(f"        {h}")
        failed += not ok
    print(f"\nlogin check: {len(o.users) - failed}/{len(o.users)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
