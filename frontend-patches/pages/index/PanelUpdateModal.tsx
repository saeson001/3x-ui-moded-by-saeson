import { useTranslation } from 'react-i18next';
import { Alert, Button, Modal, Tag } from 'antd';
import { CloudDownloadOutlined } from '@ant-design/icons';

import { formatPanelVersion } from '@/lib/panel-version';
import './PanelUpdateModal.css';

// saeson mod version, injected at build time (see build.yml)
const SAESON_MOD_VERSION = '__SAESON_MOD_VERSION__';

export interface PanelUpdateInfo {
  channel?: string;
  currentVersion: string;
  latestVersion: string;
  currentCommit?: string;
  latestCommit?: string;
  updateAvailable: boolean;
}

interface PanelUpdateModalProps {
  open: boolean;
  info: PanelUpdateInfo;
  onClose: () => void;
}

export default function PanelUpdateModal({ open, info, onClose }: PanelUpdateModalProps) {
  const { t } = useTranslation();

  return (
    <Modal open={open} title={t('pages.index.updatePanel')} footer={null} onCancel={onClose}>
      {/* saeson mod: in-panel update is DISABLED for the fork.
          The backend /panel/api/server/updatePanel pulls the OFFICIAL MHSanaei
          build and would overwrite this mod (Clash Link / traffic monitor /
          firewall / i18n / scheduled traffic reset all lost). Upgrading the mod
          must go through the saeson repo's install.sh instead. */}
      <Alert
        type="info"
        className="mb-12"
        showIcon
        message="本修改版不支持面板内一键更新"
        description="面板内置的更新会从官方 MHSanaei/3x-ui 仓库拉取官方二进制并覆盖本修改版，导致 Clash Link / 流量监控 / 防火墙 / 汉化 / 流量定时重置等全部丢失。升级 mod 请使用 saeson 仓库的 install.sh："
      />

      <pre
        style={{
          background: 'rgba(0,0,0,0.04)',
          padding: '10px 12px',
          borderRadius: 6,
          overflowX: 'auto',
          fontSize: 13,
          lineHeight: 1.6,
          margin: '8px 0 16px',
        }}
      >
        bash &lt;(curl -fsSL https://raw.githubusercontent.com/saeson001/3x-ui-moded-by-saeson/main/install.sh)&gt;
      </pre>

      <div className="version-list">
        <div className="version-list-item">
          <span>saeson mod</span>
          <Tag color="geekblue">{SAESON_MOD_VERSION}</Tag>
        </div>
        <div className="version-list-item">
          <span>当前面板内核版本</span>
          <Tag color="green">{formatPanelVersion(window.X_UI_CUR_VER || info.currentVersion) || '?'}</Tag>
        </div>
      </div>

      <div className="actions-row">
        <Button type="primary" disabled icon={<CloudDownloadOutlined />}>
          请使用 install.sh 更新（面板内更新已禁用）
        </Button>
      </div>
    </Modal>
  );
}
