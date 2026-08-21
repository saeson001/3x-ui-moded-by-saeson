import { useEffect, useState } from 'react';
import type { CSSProperties } from 'react';
import { Card, Col, Row, Statistic, Tooltip } from 'antd';
import {
  ArrowUpOutlined,
  ArrowDownOutlined,
  ThunderboltOutlined,
  DesktopOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';

import { HttpUtil, SizeFormatter } from '@/utils';
import { useMediaQuery } from '@/hooks/useMediaQuery';

interface NetStatsObj {
  net: { up: number; down: number; sent: number; recv: number };
  xray: { up: number; down: number; upTotal: number; downTotal: number };
  system: { up: number; down: number };
}

const smallStyle: CSSProperties = {
  marginTop: 10,
  color: 'rgba(128,128,128,.9)',
  fontSize: 12,
};

export default function SaesonTrafficCard() {
  const { isMobile } = useMediaQuery();
  const [stats, setStats] = useState<NetStatsObj | null>(null);

  useEffect(() => {
    let alive = true;
    const load = () => {
      HttpUtil.get<NetStatsObj>('/panel/api/netstats')
        .then((msg) => {
          if (alive && msg?.success && msg.obj) setStats(msg.obj);
        })
        .catch(() => {});
    };
    load();
    const timer = setInterval(load, 1000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, []);

  const xray = stats?.xray || { up: 0, down: 0, upTotal: 0, downTotal: 0 };
  const system = stats?.system || { up: 0, down: 0 };

  return (
    <>
      <Col xs={24} lg={12}>
        <Card
          title={
            <span>
              Xray 代理流量&nbsp;
              <Tooltip title="经过 3x-ui 入站节点转发的流量，即面板统计口径（应用层字节，不含 TCP/TLS 开销）">
                <QuestionCircleOutlined style={{ opacity: 0.5 }} />
              </Tooltip>
            </span>
          }
          hoverable
        >
          <Row gutter={isMobile ? [8, 8] : 0}>
            <Col span={12}>
              <Statistic
                title="上传"
                value={SizeFormatter.sizeFormat(xray.up)}
                prefix={<ThunderboltOutlined />}
                suffix="/s"
              />
            </Col>
            <Col span={12}>
              <Statistic
                title="下载"
                value={SizeFormatter.sizeFormat(xray.down)}
                prefix={<ThunderboltOutlined />}
                suffix="/s"
              />
            </Col>
          </Row>
          <div style={smallStyle}>
            累计：↑ {SizeFormatter.sizeFormat(xray.upTotal)} ／ ↓{' '}
            {SizeFormatter.sizeFormat(xray.downTotal)}（面板记账，Xray 重启可能丢失部分统计）
          </div>
        </Card>
      </Col>

      <Col xs={24} lg={12}>
        <Card
          title={
            <span>
              系统流量（非代理）&nbsp;
              <Tooltip title="网卡总流量减去 Xray 代理流量 ≈ SSH/扫描/攻击/系统更新等 VPS 自身流量。VPS 商按网卡总流量（本页'整体速度'+'总数据'）计费">
                <QuestionCircleOutlined style={{ opacity: 0.5 }} />
              </Tooltip>
            </span>
          }
          hoverable
        >
          <Row gutter={isMobile ? [8, 8] : 0}>
            <Col span={12}>
              <Statistic
                title="上传"
                value={SizeFormatter.sizeFormat(system.up)}
                prefix={<ArrowUpOutlined />}
                suffix="/s"
              />
            </Col>
            <Col span={12}>
              <Statistic
                title="下载"
                value={SizeFormatter.sizeFormat(system.down)}
                prefix={<ArrowDownOutlined />}
                suffix="/s"
              />
            </Col>
          </Row>
          <div style={smallStyle}>
            <DesktopOutlined /> 若此数值长期偏高，说明 VPS 有被扫描/爆破或异常进程，建议开启下方防火墙
          </div>
        </Card>
      </Col>
    </>
  );
}
