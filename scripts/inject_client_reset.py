#!/usr/bin/env python3
"""
inject_client_reset.py — Add per-client scheduled traffic reset to the 3x-ui
client page without forking the whole ClientsPage.tsx.

It:
1. Copies the new ClientResetModal.tsx into the frontend source.
2. Patches frontend/src/pages/clients/ClientsPage.tsx at 4 well-known anchors
   (lazy import, state vars, a row-action button, and the modal mount) so the
   "流量定时重置" button appears next to the existing QR button.

Usage:
    python3 inject_client_reset.py <project_root> <build_root>
"""
import os
import shutil
import sys


def patch_clients_page(path):
    if not os.path.exists(path):
        print(f"Error: {path} not found")
        return False
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    if 'ClientResetModal' in content:
        print("client-reset already injected into ClientsPage.tsx. Skipping.")
        return True

    original = content

    # 1) lazy import after ClientQrModal
    anchor1 = "const ClientQrModal = lazy(() => import('./ClientQrModal'));"
    inject1 = anchor1 + "\nconst ClientResetModal = lazy(() => import('./ClientResetModal'));"
    if anchor1 not in content:
        print("Error: lazy import anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor1, inject1, 1)

    # 2) state vars after qrClient state
    anchor2 = "const [qrClient, setQrClient] = useState<ClientRecord | null>(null);"
    inject2 = anchor2 + "\n  const [resetOpen, setResetOpen] = useState(false);\n  const [resetClient, setResetClient] = useState<ClientRecord | null>(null);"
    if anchor2 not in content:
        print("Error: qrClient state anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor2, inject2, 1)

    # 3) row-action button before the clientInfo Tooltip (right after QR Tooltip)
    anchor3 = "          <Tooltip title={t('pages.clients.clientInfo')}>"
    inject3 = (
        "          <Tooltip title={'流量定时重置'}>\n"
        "            <Button size=\"small\" type=\"text\" style={{ fontSize: 16 }} icon={<RestOutlined />} aria-label={'流量定时重置'} onClick={() => { setResetClient(record); setResetOpen(true); }} />\n"
        "          </Tooltip>\n"
        + anchor3
    )
    if anchor3 not in content:
        print("Error: clientInfo Tooltip anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor3, inject3, 1)

    # 4) modal mount after the ClientQrModal LazyMount
    anchor4 = "            onOpenChange={setQrOpen}\n          />\n        </LazyMount>"
    inject4 = (
        "            onOpenChange={setQrOpen}\n"
        "          />\n"
        "        </LazyMount>\n"
        "        <LazyMount when={resetOpen}>\n"
        "          <ClientResetModal open={resetOpen} client={resetClient} onOpenChange={setResetOpen} />\n"
        "        </LazyMount>"
    )
    if anchor4 not in content:
        print("Error: ClientQrModal mount anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor4, inject4, 1)

    if content == original:
        print("Error: no changes applied to ClientsPage.tsx")
        return False

    with open(path, 'w', encoding='utf-8') as f:
        f.write(content)
    print("Successfully injected client-reset UI into ClientsPage.tsx")
    return True


def main():
    if len(sys.argv) < 3:
        print("Usage: inject_client_reset.py <project_root> <build_root>")
        sys.exit(1)
    project_root = sys.argv[1]
    build_root = sys.argv[2]

    # copy the modal
    modal_src = os.path.join(project_root, 'frontend-patches', 'pages', 'clients', 'ClientResetModal.tsx')
    modal_dst = os.path.join(build_root, 'frontend', 'src', 'pages', 'clients', 'ClientResetModal.tsx')
    if os.path.exists(modal_src):
        os.makedirs(os.path.dirname(modal_dst), exist_ok=True)
        shutil.copy2(modal_src, modal_dst)
        print(f"  Copied: frontend/src/pages/clients/ClientResetModal.tsx")
    else:
        print(f"Warning: {modal_src} not found, skipping modal copy")

    # patch the page
    page = os.path.join(build_root, 'frontend', 'src', 'pages', 'clients', 'ClientsPage.tsx')
    if not patch_clients_page(page):
        sys.exit(1)

    print("=== client-reset frontend injection complete ===")


if __name__ == '__main__':
    main()
