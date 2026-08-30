#!/usr/bin/env python3
"""
inject_saeson_routes.py — Inject netstats + firewall modules into 3x-ui build

This script:
1. Copies netstats / firewall module files and the controller into place
2. Registers the routes in internal/web/web.go (after NewAPIController)

Usage:
    python3 inject_saeson_routes.py <project_root> <build_root>
"""
import os
import shutil
import sys


def copy_tree(src_dir, dst_dir):
    if not os.path.exists(src_dir):
        print(f"Warning: {src_dir} not found. Skipping.")
        return False
    src_abs = os.path.abspath(src_dir)
    dst_abs = os.path.abspath(dst_dir)
    os.makedirs(dst_dir, exist_ok=True)
    if src_abs == dst_abs:
        for f in os.listdir(src_dir):
            if f.endswith('.go'):
                print(f"  Already present: {dst_dir}/{f}")
        return True
    for f in os.listdir(src_dir):
        if f.endswith('.go'):
            shutil.copy2(os.path.join(src_dir, f), os.path.join(dst_dir, f))
            print(f"  Copied: {dst_dir}/{f}")
    return True


def inject_register(web_go_path):
    if not os.path.exists(web_go_path):
        print(f"Error: {web_go_path} not found")
        return False

    with open(web_go_path, 'r', encoding='utf-8') as f:
        content = f.read()

    if 'RegisterSaesonRoutes' in content:
        print("Saeson routes already registered in web.go. Skipping.")
        return True

    lines = content.splitlines(keepends=True)

    injection_code = (
        '\t// --- moded by saeson: netstats + firewall + client scheduled reset ---\n'
        '\tcontroller.RegisterSaesonRoutes(g.Group("/panel/api"), database.GetDB(), s.api)\n'
        '\t// --- end saeson routes ---\n\n'
    )

    injected = False
    new_lines = []
    for line in lines:
        new_lines.append(line)
        if 'controller.NewAPIController(g)' in line and not injected:
            new_lines.append(injection_code)
            injected = True

    if not injected:
        print("Error: could not find 'controller.NewAPIController(g)' in web.go")
        return False

    # Make sure required imports exist (controller & database are almost
    # certainly imported by upstream web.go already; add if missing).
    def add_import(lines, pkg_path):
        imp = f'\t"{pkg_path}"\n'
        for line in lines:
            if pkg_path in line:
                return lines
        for i, line in enumerate(lines):
            if line.strip() == ')' and i > 5:
                return lines[:i] + [imp] + lines[i:]
        return lines

    new_lines = add_import(new_lines, 'github.com/mhsanaei/3x-ui/v3/internal/web/controller')
    new_lines = add_import(new_lines, 'github.com/mhsanaei/3x-ui/v3/internal/database')

    with open(web_go_path, 'w', encoding='utf-8') as f:
        f.writelines(new_lines)

    print(f"Successfully injected saeson route registration into {web_go_path}")
    return True


def main():
    if len(sys.argv) < 3:
        print("Usage: inject_saeson_routes.py <project_root> <build_root>")
        sys.exit(1)

    project_root = sys.argv[1]
    build_root = sys.argv[2]

    print("=== Injecting saeson netstats + firewall modules ===")

    print("\n1. Copying module files...")
    copy_tree(os.path.join(project_root, 'internal', 'netstats'),
              os.path.join(build_root, 'internal', 'netstats'))
    copy_tree(os.path.join(project_root, 'internal', 'firewall'),
              os.path.join(build_root, 'internal', 'firewall'))
    copy_tree(os.path.join(project_root, 'internal', 'trafficlog'),
              os.path.join(build_root, 'internal', 'trafficlog'))
    copy_tree(os.path.join(project_root, 'internal', 'clientreset'),
              os.path.join(build_root, 'internal', 'clientreset'))
    # Controller file lives under internal/web/controller in our repo too,
    # but the CI prepare step already copies it; still handle standalone case.
    ctrl_src = os.path.join(project_root, 'internal', 'web', 'controller', 'saeson_routes.go')
    ctrl_dst = os.path.join(build_root, 'internal', 'web', 'controller', 'saeson_routes.go')
    if os.path.abspath(ctrl_src) != os.path.abspath(ctrl_dst):
        if os.path.exists(ctrl_src):
            os.makedirs(os.path.dirname(ctrl_dst), exist_ok=True)
            shutil.copy2(ctrl_src, ctrl_dst)
            print(f"  Copied: internal/web/controller/saeson_routes.go")
    else:
        print("  Already present: internal/web/controller/saeson_routes.go")

    print("\n2. Injecting route registration into web.go...")
    web_go_path = os.path.join(build_root, 'internal', 'web', 'web.go')
    if not inject_register(web_go_path):
        sys.exit(1)

    print("\n=== saeson module injection complete ===")


if __name__ == '__main__':
    main()
