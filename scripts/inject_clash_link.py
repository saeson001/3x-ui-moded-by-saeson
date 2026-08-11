#!/usr/bin/env python3
"""
inject_clash_link.py — Inject Clash Link route registration into 3x-ui web.go

This script:
1. Copies clash_link module files to the correct locations
2. Adds clash_link route registration to internal/web/web.go
3. Adds necessary imports

Usage:
    python3 inject_clash_link.py <project_root> <upstream_checkout_dir>
"""
import os
import re
import shutil
import sys


def add_import(lines, pkg_path, alias=""):
    """Add an import if not already present. Returns (modified, lines)."""
    import_line = f'\t"{pkg_path}"'
    if alias:
        import_line = f'\t{alias} "{pkg_path}"'

    # Check if already imported
    for line in lines:
        if pkg_path in line:
            return False, lines  # Already present

    # Find the last import line (ends with closing paren on its own line)
    for i, line in enumerate(lines):
        if line.strip() == ')' and i > 5:
            # Insert before this closing paren
            new_lines = lines[:i] + [import_line + '\n'] + lines[i:]
            return True, new_lines
    return False, lines


def inject_clash_link_register(web_go_path):
    """Inject clash link route registration into web.go"""
    if not os.path.exists(web_go_path):
        print(f"Error: {web_go_path} not found")
        return False

    with open(web_go_path, 'r', encoding='utf-8') as f:
        content = f.read()

    # Check if already injected
    if 'clash_link' in content:
        print("Clash link registration already exists in web.go. Skipping injection.")
        return True

    lines = content.splitlines(keepends=True)

    # 1. Add import for clash_link controller
    changed, lines = add_import(
        lines,
        'github.com/mhsanaei/3x-ui/v3/internal/web/controller',
    )
    if not changed:
        # The import should already exist for other controllers
        pass

    changed2, lines = add_import(
        lines,
        'github.com/mhsanaei/3x-ui/v3/internal/database',
    )

    # 2. Find the line after s.api = controller.NewAPIController(g)
    # and inject our registration code
    injection_code = (
        '\t// --- moded by saeson: Clash link subscription ---\n'
        '\tif err := controller.RegisterClashLinkRoutes(\n'
        '\t\tg.Group("/panel/api"),\n'
        '\t\tengine.Group("/"),\n'
        '\t\tdatabase.GetDB(),\n'
        '\t\t"/etc/x-ui/clash-configs",\n'
        '\t); err != nil {\n'
        '\t\tlogger.Warning("Failed to register clash link routes:", err)\n'
        '\t}\n'
        '\t// --- end clash link ---\n\n'
    )

    injected = False
    new_lines = []
    for i, line in enumerate(lines):
        new_lines.append(line)
        if 'controller.NewAPIController(g)' in line and not injected:
            new_lines.append(injection_code)
            injected = True

    if not injected:
        print("Warning: Could not find 'controller.NewAPIController(g)' in web.go")
        print("Trying alternative: injecting before initRouter end...")
        # Alternative: find the end of initRouter function
        new_lines = []
        in_init_router = False
        brace_depth = 0
        injected = False

        for i, line in enumerate(lines):
            if 'func (s *Server) initRouter' in line:
                in_init_router = True
            new_lines.append(line)
            if in_init_router:
                brace_depth += line.count('{') - line.count('}')
                if brace_depth <= 1 and '{' not in line and i > 10 and not injected:
                    # Insert before the closing brace
                    new_lines.insert(-1, injection_code)
                    injected = True
                    in_init_router = False

    if not injected:
        print("Error: Could not inject clash link registration")
        return False

    # 3. Also need to add "logger" import if we use logger.Warning
    # The upstream web.go likely already imports a logger. Let's check.
    # If not, we'll add it
    changed3, new_lines = add_import(
        new_lines,
        'github.com/mhsanaei/3x-ui/v3/internal/logger',
    )

    with open(web_go_path, 'w', encoding='utf-8') as f:
        f.writelines(new_lines)

    print(f"Successfully injected clash link registration into {web_go_path}")
    return True


def copy_clash_link_module(project_root, upstream_dir):
    """Copy clash_link module files to upstream checkout"""
    src_dir = os.path.join(project_root, 'internal', 'clash_link')
    dst_dir = os.path.join(upstream_dir, 'internal', 'clash_link')

    if not os.path.exists(src_dir):
        print(f"Warning: {src_dir} not found. Skipping clash_link copy.")
        return False

    os.makedirs(dst_dir, exist_ok=True)

    for f in os.listdir(src_dir):
        if f.endswith('.go'):
            src = os.path.join(src_dir, f)
            dst = os.path.join(dst_dir, f)
            shutil.copy2(src, dst)
            print(f"  Copied: {f}")

    # Also copy the route registration file
    routes_src = os.path.join(project_root, 'internal', 'web', 'controller', 'clash_link_routes.go')
    routes_dst = os.path.join(upstream_dir, 'internal', 'web', 'controller', 'clash_link_routes.go')
    if os.path.exists(routes_src):
        shutil.copy2(routes_src, routes_dst)
        print(f"  Copied: clash_link_routes.go")

    return True


def main():
    if len(sys.argv) < 3:
        print("Usage: inject_clash_link.py <project_root> <upstream_checkout_dir>")
        sys.exit(1)

    project_root = sys.argv[1]
    upstream_dir = sys.argv[2]

    print("=== Injecting Clash Link module ===")

    # Step 1: Copy files
    print("\n1. Copying clash_link module files...")
    copy_clash_link_module(project_root, upstream_dir)

    # Step 2: Inject route registration into web.go
    print("\n2. Injecting route registration into web.go...")
    web_go_path = os.path.join(upstream_dir, 'internal', 'web', 'web.go')
    if os.path.exists(web_go_path):
        inject_clash_link_register(web_go_path)
    else:
        print(f"Error: web.go not found at {web_go_path}")
        sys.exit(1)

    print("\n=== Clash Link injection complete ===")


if __name__ == '__main__':
    main()
