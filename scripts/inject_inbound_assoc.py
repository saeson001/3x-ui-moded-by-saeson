#!/usr/bin/env python3
"""
inject_inbound_assoc.py — Add "batch set inbound association" (批量设置关联入站,
replace semantics) to the 3x-ui client page without forking the whole
ClientsPage.tsx.

It:
1. Copies the new BulkSetInboundsModal.tsx into the frontend source.
2. Patches frontend/src/pages/clients/ClientsPage.tsx at 4 well-known anchors
   (lazy import, state var, a "更多" dropdown menu item, and the modal mount)
   so the "设置关联入站" action appears next to the existing attach/detach
   items (only when rows are selected).

Usage:
    python3 inject_inbound_assoc.py <project_root> <build_root>
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
    if 'BulkSetInboundsModal' in content:
        print("inbound-assoc already injected into ClientsPage.tsx. Skipping.")
        return True

    original = content

    # 1) lazy import after BulkAttachInboundsModal
    anchor1 = "const BulkAttachInboundsModal = lazy(() => import('./BulkAttachInboundsModal'));"
    inject1 = anchor1 + "\nconst BulkSetInboundsModal = lazy(() => import('./BulkSetInboundsModal'));"
    if anchor1 not in content:
        print("Error: lazy import anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor1, inject1, 1)

    # 2) state var after bulkAttachOpen state
    anchor2 = "  const [bulkAttachOpen, setBulkAttachOpen] = useState(false);"
    inject2 = anchor2 + "\n  const [bulkSetOpen, setBulkSetOpen] = useState(false);"
    if anchor2 not in content:
        print("Error: bulkAttachOpen state anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor2, inject2, 1)

    # 3) dropdown menu item: insert "设置关联入站" right after the detach item
    #    (before the addToGroup item) inside the selected-rows branch.
    anchor3 = (
        "                                  {\n"
        "                                    key: 'detach',\n"
        "                                    icon: <UsergroupDeleteOutlined />,\n"
        "                                    label: t('pages.clients.detach'),\n"
        "                                    danger: true,\n"
        "                                    onClick: () => setBulkDetachOpen(true),\n"
        "                                  },\n"
        "                                  {\n"
        "                                    key: 'addToGroup',"
    )
    inject3 = (
        "                                  {\n"
        "                                    key: 'detach',\n"
        "                                    icon: <UsergroupDeleteOutlined />,\n"
        "                                    label: t('pages.clients.detach'),\n"
        "                                    danger: true,\n"
        "                                    onClick: () => setBulkDetachOpen(true),\n"
        "                                  },\n"
        "                                  {\n"
        "                                    key: 'setAssoc',\n"
        "                                    icon: <RetweetOutlined />,\n"
        "                                    label: '设置关联入站',\n"
        "                                    onClick: () => setBulkSetOpen(true),\n"
        "                                  },\n"
        "                                  {\n"
        "                                    key: 'addToGroup',"
    )
    if anchor3 not in content:
        print("Error: detach/detach menu anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor3, inject3, 1)

    # 4) modal mount after the BulkAttachInboundsModal LazyMount (before BulkDetach)
    anchor4 = "        </LazyMount>\n        <LazyMount when={bulkDetachOpen}>"
    inject4 = (
        "        </LazyMount>\n"
        "        <LazyMount when={bulkSetOpen}>\n"
        "          <BulkSetInboundsModal\n"
        "            open={bulkSetOpen}\n"
        "            count={selectedRowKeys.length}\n"
        "            emails={[...selectedRowKeys]}\n"
        "            inbounds={inbounds}\n"
        "            onOpenChange={setBulkSetOpen}\n"
        "          />\n"
        "        </LazyMount>\n"
        "        <LazyMount when={bulkDetachOpen}>"
    )
    if anchor4 not in content:
        print("Error: BulkAttach LazyMount anchor not found in ClientsPage.tsx")
        return False
    content = content.replace(anchor4, inject4, 1)

    if content == original:
        print("Error: no changes applied to ClientsPage.tsx")
        return False

    with open(path, 'w', encoding='utf-8') as f:
        f.write(content)
    print("Successfully injected inbound-assoc UI into ClientsPage.tsx")
    return True


def main():
    if len(sys.argv) < 3:
        print("Usage: inject_inbound_assoc.py <project_root> <build_root>")
        sys.exit(1)
    project_root = sys.argv[1]
    build_root = sys.argv[2]

    # copy the modal
    modal_src = os.path.join(project_root, 'frontend-patches', 'pages', 'clients', 'BulkSetInboundsModal.tsx')
    modal_dst = os.path.join(build_root, 'frontend', 'src', 'pages', 'clients', 'BulkSetInboundsModal.tsx')
    if os.path.exists(modal_src):
        os.makedirs(os.path.dirname(modal_dst), exist_ok=True)
        shutil.copy2(modal_src, modal_dst)
        print(f"  Copied: frontend/src/pages/clients/BulkSetInboundsModal.tsx")
    else:
        print(f"Warning: {modal_src} not found, skipping modal copy")

    # patch the page
    page = os.path.join(build_root, 'frontend', 'src', 'pages', 'clients', 'ClientsPage.tsx')
    if not patch_clients_page(page):
        sys.exit(1)

    print("=== inbound-assoc frontend injection complete ===")


if __name__ == '__main__':
    main()
