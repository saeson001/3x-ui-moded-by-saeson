import { useCallback, useEffect, useState } from 'react';
import { Button, Card, Col, InputNumber, Modal, Popconfirm, Row, Space, Tag, message } from 'antd';
import {
  SafetyCertificateOutlined,
  SafetyOutlined,
  SyncOutlined,
  PlusOutlined,
} from '@ant-design/icons';

import { HttpUtil } from '@/utils';

interface FirewallStatus {
  backend: string;
  backendFound: boolean;
  active: boolean;
  sshPorts: number[];
  panelPorts: number[];
  inboundPorts: number[];
  extraPorts: number[];
  allPorts: number[];
  notice?: string;
}

export default function SaesonFirewallCard() {
  const [status, setStatus] = useState<FirewallStatus | null>(null);
  const [manageOpen, setManageOpen] = useState(false);
  const [newPort, setNewPort] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    HttpUtil.get<FirewallStatus>('/panel/api/firewall/status')
      .then((msg) => {
        if (msg?.success && msg.obj) setStatus(msg.obj);
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const doEnable = async () => {
    setBusy(true);
    try {
      const res = await HttpUtil.post<FirewallStatus>('/panel/api/firewall/enable', {});
      if (res?.success && res.obj) {
        setStatus(res.obj);
        message.success('防火墙已开启：仅放行 SSH / 面板 / 节点 / 自定义端口');
      } else {
        message.error(res?.msg || '开启失败');
      }
    } finally {
      setBusy(false);
    }
  };

  const doDisable = async () => {
    setBusy(true);
    try {
      const res = await HttpUtil.post<FirewallStatus>('/panel/api/firewall/disable', {});
      if (res?.success && res.obj) {
        setStatus(res.obj);
        message.success('防火墙已关闭');
      } else {
        message.error(res?.msg || '关闭失败');
      }
    } finally {
      setBusy(false);
    }
  };

  const doSync = async () => {
    setBusy(true);
    try {
      const res = await HttpUtil.post<FirewallStatus>('/panel/api/firewall/sync', {});
      if (res?.success && res.obj) {
        setStatus(res.obj);
        message.success('端口已同步（新增节点端口已放行）');
      } else {
        message.error(res?.msg || '同步失败');
      }
    } finally {
      setBusy(false);
    }
  };

  const doPortAction = async (port: number, action: 'add' | 'remove') => {
    setBusy(true);
    try {
      const res = await HttpUtil.post<FirewallStatus>('/panel/api/firewall/ports', {
        port,
        action,
      });
      if (res?.success && res.obj) {
        setStatus(res.obj);
        setNewPort(null);
        message.success(action === 'add' ? `已放行端口 ${port}` : `已移除端口 ${port}`);
      } else {
        message.error(res?.msg || '操作失败');
      }
    } finally {
      setBusy(false);
    }
  };

  const portTags = (ports: number[], color: string, closable = false) =>
    ports.map((p) => (
      <Tag
        key={p}
        color={color}
        closable={closable}
        onClose={(e) => {
          e.preventDefault();
          doPortAction(p, 'remove');
        }}
      >
        {p}
      </Tag>
    ));

  const st = status;

  return (
    <Col xs={24} lg={12}>
      <Card
        title={
          <span>
            <SafetyCertificateOutlined /> 防火墙
          </span>
        }
        hoverable
        extra={
          st ? (
            <Tag color={st.active ? 'green' : 'default'}>
              {st.active ? '已开启' : '未开启'}
            </Tag>
          ) : null
        }
      >
        {st && !st.backendFound && (
          <div style={{ marginBottom: 12 }}>
            <Tag color="orange">未检测到 ufw / firewalld</Tag>
            <div style={{ marginTop: 8, fontSize: 12, color: '#888' }}>{st.notice}</div>
          </div>
        )}
        {st && st.backendFound && (
          <>
            <div style={{ marginBottom: 12 }}>
              后端：<Tag color="blue">{st.backend}</Tag>
              &nbsp; SSH：<Tag color="cyan">{(st.sshPorts || []).join(' / ')}</Tag>
              &nbsp; 面板：<Tag color="geekblue">{(st.panelPorts || []).join(' / ')}</Tag>
            </div>
            <div style={{ fontSize: 12, color: '#888', marginBottom: 12 }}>
              已放行 {st.allPorts.length} 个端口（SSH + 面板 + {st.inboundPorts.length} 个节点端口
              {st.extraPorts.length > 0 ? ` + ${st.extraPorts.length} 个自定义` : ''}），
              其余入站一律拒绝，出站不受影响。
            </div>
          </>
        )}
        <Space wrap>
          {st?.active ? (
            <Popconfirm
              title="确认关闭防火墙？"
              description="关闭后所有端口都将开放"
              onConfirm={doDisable}
            >
              <Button danger loading={busy}>
                关闭防火墙
              </Button>
            </Popconfirm>
          ) : (
            <Popconfirm
              title="一键开启防火墙？"
              description="将自动放行 SSH / 面板 / 全部节点端口，其余入站拒绝（出站不受影响）"
              onConfirm={doEnable}
            >
              <Button type="primary" icon={<SafetyOutlined />} loading={busy}>
                一键开启
              </Button>
            </Popconfirm>
          )}
          <Button icon={<SyncOutlined />} onClick={doSync} loading={busy} disabled={!st?.active}>
            同步端口
          </Button>
          <Button onClick={() => setManageOpen(true)} disabled={!st?.backendFound}>
            管理端口
          </Button>
        </Space>
      </Card>

      <Modal
        title="防火墙端口管理"
        open={manageOpen}
        onCancel={() => setManageOpen(false)}
        footer={null}
        width={640}
      >
        {st && (
          <>
            <p style={{ marginBottom: 6 }}>
              <b>SSH 端口</b>（自动检测，不可移除）
            </p>
            <div style={{ marginBottom: 12 }}>{portTags(st.sshPorts, 'cyan')}</div>
            <p style={{ marginBottom: 6 }}>
              <b>面板 / 订阅端口</b>（自动读取，不可移除）
            </p>
            <div style={{ marginBottom: 12 }}>{portTags(st.panelPorts, 'geekblue')}</div>
            <p style={{ marginBottom: 6 }}>
              <b>节点端口</b>（来自入站列表，新增节点后请点“同步端口”）
            </p>
            <div style={{ marginBottom: 12 }}>{portTags(st.inboundPorts, 'green')}</div>
            <p style={{ marginBottom: 6 }}>
              <b>自定义端口</b>（可移除）
            </p>
            <div style={{ marginBottom: 16 }}>
              {st.extraPorts.length > 0 ? (
                portTags(st.extraPorts, 'orange', true)
              ) : (
                <span style={{ color: '#999' }}>暂无</span>
              )}
            </div>
            <Row gutter={8}>
              <Col>
                <InputNumber
                  min={1}
                  max={65535}
                  value={newPort}
                  placeholder="端口号"
                  onChange={(v) => setNewPort(v)}
                />
              </Col>
              <Col>
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  disabled={!newPort}
                  loading={busy}
                  onClick={() => newPort && doPortAction(newPort, 'add')}
                >
                  添加端口
                </Button>
              </Col>
            </Row>
          </>
        )}
      </Modal>
    </Col>
  );
}
