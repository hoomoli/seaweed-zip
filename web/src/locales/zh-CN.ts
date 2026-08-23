import type {Translation} from './en';
export const zhCN:Translation = {
 signOut:'退出',signIn:'重新登录',
 states:{QUEUED:'排队',VALIDATING:'验证授权',PACKING:'源读取 / 打包',READY:'可下载',DELETING:'清理中',DELETED:'已清理',WAITING_AUTH:'等待补授权',FAILED:'失败',CANCELLED:'已取消'},
 cleaned:'已清理',cleaning:'清理中',notCreated:'尚未生成',pending:'等待清理',hours:'小时后到期',objects:'对象',
 release:'游戏 / 版本',id:'包标识',node:'请求节点',phase:'阶段',progress:'实际进度',ack:'消费确认',downloading:'下载中',size:'ZIP 大小',retention:'产物保留',history:'记录保留至',
 boundary:'OIDC · 管理员会话',headline:'资源成包，传输有据。',intro:'源读取 → 校验打包 → 下载 → 消费确认。每个阶段独立追踪。',minimum:'产物最短保留',tasks:'打包任务',filter:'阶段筛选',all:'所有阶段',refresh:'刷新',readFailed:'读取失败',empty:'暂无打包任务',page:'页',perPage:'每页 25 条',previous:'上一页',next:'下一页',notAck:'发送完成 ≠ 消费方已校验',evidence:'任务证据',generation:'执行代',expires:'产物到期',consumers:'消费方',unmarked:'未标记节点',noDeployment:'无部署标签',verified:'已校验并确认',notConfirmed:'尚未确认',downloadPending:'下载中 · 尚未确认',cancel:'取消',cancelConfirm:'仅取消此消费方，其他消费方不受影响。继续？',events:'阶段 / 事件',since:'距上事件',retry:'重试需要调用方补充有效源授权；管理页面不持有签名 URL 或消费凭证。'
};
