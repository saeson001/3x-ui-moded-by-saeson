import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Collapse, Modal, Spin, Tag } from 'antd';
import { CopyOutlined, CheckOutlined } from '@ant-design/icons';
import { HttpUtil, ClipboardManager } from '@/utils';
import { isPostQuantumLink } from '@/lib/xray/inbound-link';
import { LinkTags, linkMetaText, parseLinkParts } from '@/lib/xray/link-label';
import { QrPanel } from '@/pages/inbounds/qr';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';
import { buildWireguardClientConfig, findWireguardInbound, isWireguardClient } from './wireguardConfig';

interface SubSettings {
  enable: boolean;
  subURI: string;
  subJsonURI: string;
  subJsonEnable: boolean;
  publicHost?: string;
}

interface ClientQrModalProps {
  open: boolean;
  client: ClientRecord | null;
  inboundsById: Record<number, InboundOption>;
  subSettings?: SubSettings;
  onOpenChange: (open: boolean) => void;
}

interface ApiMsg<T = unknown> {
  success?: boolean;
  obj?: T;
}

const DEFAULT_SUB: SubSettings = { enable: false, subURI: '', subJsonURI: '', subJsonEnable: false, publicHost: '' };
const ACTIVE_KEY_STORAGE = 'clientQrModal_activeKey';

function CopyBtn({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(async (e: React.MouseEvent) => {
    e.stopPropagation();
    const ok = await ClipboardManager.copyText(text);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  }, [text]);
  return (
    <Button
      type="text"
      size="small"
      icon={copied ? <CheckOutlined /> : <CopyOutlined />}
      onClick={handleCopy}
      style={{ marginLeft: 8, opacity: 0.7 }}
      title={copied ? '已复制' : '复制'}
    />
  );
}

export default function ClientQrModal({
  open,
  client,
  inboundsById,
  subSettings = DEFAULT_SUB,
  onOpenChange,
}: ClientQrModalProps) {
  const { t } = useTranslation();
  const [links, setLinks] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);

  const subLink = useMemo(() => {
    if (!client?.subId || !subSettings?.enable || !subSettings?.subURI) return '';
    return subSettings.subURI + client.subId;
  }, [client?.subId, subSettings?.enable, subSettings?.subURI]);

  const subJsonLink = useMemo(() => {
    if (!client?.subId || !subSettings?.enable) return '';
    if (!subSettings?.subJsonEnable || !subSettings?.subJsonURI) return '';
    return subSettings.subJsonURI + client.subId;
  }, [client?.subId, subSettings?.enable, subSettings?.subJsonEnable, subSettings?.subJsonURI]);

  const subClashLink = useMemo(() => {
    if (!client?.subId || !subSettings?.enable || !subSettings?.subURI) return '';
    return subSettings.subURI.replace(/\/sub\//, '/clash/') + client.subId;
  }, [client?.subId, subSettings?.enable, subSettings?.subURI]);

  const wgInbound = useMemo(() => findWireguardInbound(client, inboundsById), [client, inboundsById]);
  const wgConfigText = useMemo(() => {
    if (!client || !wgInbound || !isWireguardClient(client)) return '';
    return buildWireguardClientConfig(client, wgInbound, window.location.hostname, subSettings?.publicHost ?? '');
  }, [client, wgInbound, subSettings?.publicHost]);

  const hasAnything = !!subLink || !!subJsonLink || !!subClashLink || !!wgConfigText || links.length > 0;

  useEffect(() => {
    if (!open || !client?.subId) {
      setLinks([]);
      return;
    }
    let cancelled = false;
    setLoading(true);
    (async () => {
      try {
        const msg = await HttpUtil.get(
          `/panel/api/clients/subLinks/${encodeURIComponent(client.subId!)}`,
        ) as ApiMsg<string[]>;
        if (!cancelled) {
          setLinks(msg?.success && Array.isArray(msg.obj) ? msg.obj : []);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [open, client?.subId]);

  const [activeKey, setActiveKey] = useState<string[]>([]);

  // Build items: links first, then subscriptions
  const items = useMemo(() => {
    const out: { key: string; label: React.ReactNode; children: React.ReactNode; copyText: string }[] = [];

    // 1. Direct links (vless/vmess/trojan/...) — shown first
    links.forEach((link, idx) => {
      const parts = parseLinkParts(link);
      const meta = parts ? linkMetaText(parts) : '';
      const label: React.ReactNode = parts ? (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
          <LinkTags parts={parts} />
          {meta && <span style={{ opacity: 0.6, fontSize: 12 }}>({meta})</span>}
        </span>
      ) : `${t('pages.clients.link')} ${idx + 1}`;
      out.push({
        key: `l${idx}`,
        label: (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span>{label}</span>
            <CopyBtn text={link} />
          </div>
        ),
        children: (
          <QrPanel
            value={link}
            remark={parts?.remark || `${client?.email || ''} #${idx + 1}`}
            showQr={!isPostQuantumLink(link)}
          />
        ),
        copyText: link,
      });
    });

    // 2. Wireguard config
    if (wgConfigText) {
      out.push({
        key: 'wg-config',
        label: (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Tag color="cyan" style={{ margin: 0 }}>{t('pages.clients.wireguardConfig')}</Tag>
            <CopyBtn text={wgConfigText} />
          </div>
        ),
        children: (
          <QrPanel
            value={wgConfigText}
            remark={client?.email || 'peer'}
            downloadName={`${client?.email || 'peer'}.conf`}
          />
        ),
        copyText: wgConfigText,
      });
    }

    // 3. Subscription links
    if (subLink) {
      out.push({
        key: 'sub',
        label: (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span>{t('subscription.title')}</span>
            <CopyBtn text={subLink} />
          </div>
        ),
        children: <QrPanel value={subLink} remark={`${client?.email || ''} — ${t('subscription.title')}`} />,
        copyText: subLink,
      });
    }
    if (subJsonLink) {
      out.push({
        key: 'subJson',
        label: (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span>{`${t('subscription.title')} (JSON)`}</span>
            <CopyBtn text={subJsonLink} />
          </div>
        ),
        children: <QrPanel value={subJsonLink} remark={`${client?.email || ''} — JSON`} />,
        copyText: subJsonLink,
      });
    }
    if (subClashLink) {
      out.push({
        key: 'subClash',
        label: (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span>{`Clash ${t('subscription.title')}`}</span>
            <CopyBtn text={subClashLink} />
          </div>
        ),
        children: <QrPanel value={subClashLink} remark={`${client?.email || ''} — Clash`} />,
        copyText: subClashLink,
      });
    }

    return out;
  }, [subLink, subJsonLink, subClashLink, wgConfigText, links, client?.email, t]);

  // Restore activeKey from localStorage on open; default to first link item
  useEffect(() => {
    if (!open) {
      setActiveKey([]);
      return;
    }
    try {
      const saved = localStorage.getItem(ACTIVE_KEY_STORAGE);
      if (saved) {
        const parsed = JSON.parse(saved);
        if (Array.isArray(parsed)) {
          const validKeys = new Set(items.map(i => i.key));
          const filtered = parsed.filter((k: string) => validKeys.has(k));
          if (filtered.length > 0) {
            setActiveKey(filtered);
            return;
          }
        }
      }
    } catch { /* ignore */ }
    setActiveKey(items.length > 0 ? [items[0].key] : []);
  }, [open, items]);

  const SUB_KEYS = new Set(['sub', 'subJson', 'subClash']);

  const handleChange = useCallback((keys: string | string[]) => {
    const next = typeof keys === 'string' ? [keys] : (keys as string[]);
    setActiveKey(next);
    try {
      // 只持久化非订阅项的展开状态；订阅项展开不保存，
      // 这样下次打开弹窗默认回到直链，不会"记忆"在订阅信息上
      const persist = next.filter(k => !SUB_KEYS.has(k));
      localStorage.setItem(ACTIVE_KEY_STORAGE, JSON.stringify(persist));
    } catch { /* ignore */ }
  }, []);

  return (
    <Modal
      open={open}
      title={client ? `${t('qrCode')} — ${client.email}` : t('qrCode')}
      footer={null}
      width={520}
      centered
      onCancel={() => onOpenChange(false)}
    >
      <Spin spinning={loading}>
        {!client?.subId && !loading && (
          <div style={{ padding: 24, textAlign: 'center', opacity: 0.6 }}>{t('pages.clients.noSubId')}</div>
        )}
        {client?.subId && !hasAnything && !loading && (
          <div style={{ padding: 24, textAlign: 'center', opacity: 0.6 }}>{t('pages.clients.noLinks')}</div>
        )}
        {hasAnything && (
          <Collapse
            activeKey={activeKey}
            onChange={handleChange}
            items={items.map(({ key, label, children }) => ({ key, label, children }))}
          />
        )}
      </Spin>
    </Modal>
  );
}
