import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Collapse, Modal } from 'antd';
import type { CollapseProps } from 'antd';
import { HttpUtil } from '@/utils';

import { Protocols } from '@/schemas/primitives';
import {
  genAllLinks,
  genWireguardConfigs,
  genWireguardLinks,
  isPostQuantumLink,
  preferPublicHost,
} from '@/lib/xray/inbound-link';
import { inboundFromDb, type DbInboundLike } from '@/lib/xray/inbound-from-db';
import QrPanel from './QrPanel';
import type { SubSettings } from '../useInbounds';

interface ClientSetting {
  email?: string;
  subId?: string;
  [k: string]: unknown;
}

interface QrCodeModalProps {
  open: boolean;
  onClose: () => void;
  dbInbound: (DbInboundLike & { remark?: string }) | null;
  client?: ClientSetting | null;
  nodeAddress?: string;
  subSettings?: SubSettings;
}

interface QrItem {
  key: string;
  header: string;
  value: string;
  downloadName?: string;
  showQr?: boolean;
  hint?: string;
}

export default function QrCodeModal({
  open,
  onClose,
  dbInbound,
  client = null,
  nodeAddress = '',
  subSettings,
}: QrCodeModalProps) {
  const { t } = useTranslation();
  const [links, setLinks] = useState<{ remark?: string; link: string }[]>([]);
  const [wireguardConfigs, setWireguardConfigs] = useState<string[]>([]);
  const [wireguardLinks, setWireguardLinks] = useState<string[]>([]);
  const [subLink, setSubLink] = useState('');
  const [subJsonLink, setSubJsonLink] = useState('');
  const [subClashLink, setSubClashLink] = useState('');
  const [activeKey, setActiveKey] = useState<string[]>([]);
  // 上游 SubSettings 映射丢弃了 subClashEnable, 打开弹窗时单独拉取;
  // null 表示未知(拉取中或失败), 未知时保持旧行为照常显示链接
  const [clashEnabled, setClashEnabled] = useState<boolean | null>(null);

  useEffect(() => {
    if (!open || !dbInbound) return;
    const inbound = inboundFromDb(dbInbound);
    const fallbackHostname = preferPublicHost(window.location.hostname, subSettings?.publicHost ?? '');
    if (inbound.protocol === Protocols.WIREGUARD) {
      const peerRemark = client?.email
        ? `${dbInbound.remark}-${client.email}`
        : dbInbound.remark || '';
      setWireguardConfigs(
        genWireguardConfigs({
          inbound,
          remark: peerRemark,
          hostOverride: nodeAddress,
          fallbackHostname,
        }).split('\r\n'),
      );
      setWireguardLinks(
        genWireguardLinks({
          inbound,
          remark: peerRemark,
          hostOverride: nodeAddress,
          fallbackHostname,
        }).split('\r\n'),
      );
      setLinks([]);
    } else {
      setLinks(
        genAllLinks({
          inbound,
          remark: dbInbound.remark || '',
          client: client ?? {},
          hostOverride: nodeAddress,
          fallbackHostname,
        }),
      );
      setWireguardConfigs([]);
      setWireguardLinks([]);
    }

    const subId = client?.subId;
    let nextSub = '';
    let nextSubJson = '';
    let nextSubClash = '';
    if (subSettings?.enable && subId) {
      nextSub = (subSettings.subURI || '') + subId;
      nextSubJson = subSettings.subJsonEnable ? (subSettings.subJsonURI || '') + subId : '';
      if (clashEnabled !== false) {
        nextSubClash = (subSettings.subURI || '').replace(/\/sub\//, '/clash/') + subId;
      }
    }
    setSubLink(nextSub);
    setSubJsonLink(nextSubJson);
    setSubClashLink(nextSubClash);
  }, [open, dbInbound, client, nodeAddress, subSettings, clashEnabled]);

  useEffect(() => {
    if (!open) {
      setClashEnabled(null);
      return;
    }
    let cancelled = false;
    (async () => {
      const msg = await HttpUtil.post<{ subClashEnable?: boolean }>(
        '/panel/api/setting/defaultSettings', undefined, { silent: true },
      );
      if (!cancelled) setClashEnabled(msg?.success && msg?.obj ? !!msg.obj.subClashEnable : null);
    })();
    return () => { cancelled = true; };
  }, [open]);

  const qrItems = useMemo<QrItem[]>(() => {
    const items: QrItem[] = [];
    if (subLink) {
      items.push({ key: 'sub', header: t('subscription.title'), value: subLink });
    }
    if (subJsonLink) {
      items.push({ key: 'sub-json', header: `${t('subscription.title')} (JSON)`, value: subJsonLink });
    }
    if (subClashLink) {
      items.push({ key: 'sub-clash', header: `Clash ${t('subscription.title')}`, value: subClashLink });
    }
    if (!subClashLink && clashEnabled === false && subSettings?.enable && client?.subId) {
      items.push({
        key: 'sub-clash-disabled',
        header: `Clash ${t('subscription.title')}`,
        value: '',
        hint: 'Clash / Mihomo 订阅未开启。请在 面板设置 → 订阅 中打开「Clash / Mihomo 订阅」开关并保存，然后重新打开本弹窗，此处将显示 Clash 订阅链接与二维码。',
      });
    }
    links.forEach((link, idx) => {
      items.push({ key: `l${idx}`, header: link.remark || `Link ${idx + 1}`, value: link.link });
    });
    wireguardConfigs.forEach((cfg, idx) => {
      items.push({
        key: `wc${idx}`,
        header: `Peer ${idx + 1} config`,
        value: cfg,
        downloadName: `peer-${idx + 1}.conf`,
      });
      if (wireguardLinks[idx]) {
        items.push({ key: `wl${idx}`, header: `Peer ${idx + 1} link`, value: wireguardLinks[idx], showQr: false });
      }
    });
    return items;
  }, [subLink, subJsonLink, subClashLink, links, wireguardConfigs, wireguardLinks, client?.subId, subSettings?.enable, clashEnabled, t]);

  const collapseItems: CollapseProps['items'] = useMemo(
    () => qrItems.map((item) => ({
      key: item.key,
      label: item.header,
      children: item.hint ? (
        <div style={{ padding: '18px 12px', textAlign: 'center', opacity: 0.65, lineHeight: 1.7 }}>
          {item.hint}
        </div>
      ) : (
        <QrPanel
          value={item.value}
          remark={item.header}
          downloadName={item.downloadName || ''}
          showQr={item.showQr !== false && !isPostQuantumLink(item.value)}
        />
      ),
    })),
    [qrItems],
  );

  useEffect(() => {
    if (!open) {
      setActiveKey([]);
      return;
    }
    setActiveKey(qrItems.length > 0 ? [qrItems[0].key] : []);
  }, [open, qrItems]);

  return (
    <Modal open={open} onCancel={onClose} title={t('qrCode')} footer={null} width={420} destroyOnHidden>
      {dbInbound && collapseItems && collapseItems.length > 0 && (
        <Collapse
          ghost
          activeKey={activeKey}
          onChange={(keys) => setActiveKey(typeof keys === 'string' ? [keys] : (keys as string[]))}
          items={collapseItems}
        />
      )}
    </Modal>
  );
}
