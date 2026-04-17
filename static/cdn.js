const CDN_PROVIDERS = {
    aliyun: {
        name: "阿里云",
        idLabel: "AccessKey ID：",
        secretLabel: "AccessKey Secret：",
        typeSelect: [
            "ESA",
            "CDN",
            "DCDN",
        ],
        idHelpHtml: "<a target='_blank' href='https://ram.console.aliyun.com/manage/ak?spm=5176.12818093.nav-right.dak.488716d0mHaMgg'>创建 AccessKey</a>",
        typeHelpHtml: "<a target='_blank' href='https://esa.console.aliyun.com'>ESA</a>、<a target='_blank' href='https://cdn.console.aliyun.com/overview'>CDN</a>、<a target='_blank' href='https://dcdn.console.aliyun.com/#/overview'>DCDN</a>",
        maxSources: [1, 20, 20],
        protocolTipHtml: [
            "<tip>阿里云 ESA 类型不支持自定义端口</tip>",
            "<tip>阿里云 CDN 类型不支持自定义 HTTPS 端口</tip>",
            "<tip>阿里云 DCDN 类型不支持自定义 HTTPS 端口</tip"
        ]
    },
    baiducloud: {
        name: "百度智能云",
        idLabel: "AccessKey ID：",
        secretLabel: "AccessKey Secret：",
        typeSelect: [
            "CDN",
            "DRCDN"
        ],
        idHelpHtml: "<a target='_blank' href='https://console.bce.baidu.com/iam/?_=1651763238057#/iam/accesslist'>创建 AccessKey </a>",
        typeHelpHtml: "<a target='_blank' href='https://console.bce.baidu.com/cdn#/cdn/list'>CDN</a>、<a target='_blank' href='https://console.bce.baidu.com/cdn#/cdn/list'>DRCDN</a>",
        maxSources: [10, 10],
        protocolTipHtml: [
            "<tip></tip>",
            "<tip></tip>",
            "<tip></tip>"
        ]
    },
    tencent: {
        name: "腾讯云",
        idLabel: "SecretId：",
        secretLabel: "SecretKey：",
        typeSelect: [
            "EdgeOne",
            "CDN",
        ],
        idHelpHtml: "<a target='_blank' href='https://console.dnspod.cn/account/token/apikey'>创建腾讯云 API 密钥</a>",
        typeHelpHtml: "<a target='_blank' href='https://console.cloud.tencent.com/edgeone'>EdgeOne</a>、<a target='_blank' href='https://console.cloud.tencent.com/cdn'>CDN</a>",
        maxSources: [1, 5],
        protocolTipHtml: [
            "<tip></tip>",
            "<tip>协议跟随时，不允许自定义端口号</tip>",
        ]
    },
    cloudflare: {
        name: "Cloudflare",
        idLabel: "API Token：",
        secretLabel: "",
        typeSelect: [
            "CDN",
            "DNS",
        ],
        idHelpHtml: "<a target='_blank' href='https://dash.cloudflare.com/profile/api-tokens'>创建 API 令牌 -> 编辑区域 DNS (使用模板)</a>",
        typeHelpHtml: "<strong>CDN</strong>：开启代理，流量经过 Cloudflare 加速；<strong>DNS</strong>：仅 DNS 解析，流量不经过 Cloudflare",
        maxSources: [1, 1],
        protocolTipHtml: [
            "<tip>Cloudflare 仅支持单个源站</tip>",
            "<tip>Cloudflare 仅支持单个源站</tip>",
        ]
    },
    upyun: {
        name: "又拍云",
        idLabel: "Token：",
        secretLabel: "",
        typeSelect: [
            "CDN",
        ],
        idHelpHtml: "<a href='javascript:void(0)' onclick='openUpyunTokenDialog()'>点击自动获取 Token</a>",
        typeHelpHtml: "<a target='_blank' href='https://console.upyun.com/cdn/list/'>又拍云 CDN 控制台</a>",
        maxSources: [1],
        protocolTipHtml: [
            "<tip></tip>",
        ]
    },
}

function openUpyunTokenDialog() {
    var $ = layui.$;
    var layer = layui.layer;

    var dialogHtml = '<div style="padding:20px 24px">' +
        '<div class="layui-form-item">' +
        '<label class="layui-form-label">用户名</label>' +
        '<div class="layui-input-block">' +
        '<input type="text" id="upyun-dlg-username" class="layui-input" placeholder="又拍云账号用户名">' +
        '</div></div>' +
        '<div class="layui-form-item">' +
        '<label class="layui-form-label">密码</label>' +
        '<div class="layui-input-block" style="display:flex;gap:6px">' +
        '<input type="password" id="upyun-dlg-password" class="layui-input" placeholder="又拍云账号密码">' +
        '<button type="button" id="upyun-dlg-fetch" class="layui-btn layui-btn-sm" style="white-space:nowrap">获取 Token</button>' +
        '</div></div>' +
        '<div class="layui-form-item">' +
        '<label class="layui-form-label">Token</label>' +
        '<div class="layui-input-block" style="display:flex;gap:6px">' +
        '<input type="text" id="upyun-dlg-token" class="layui-input" placeholder="获取成功后显示" readonly>' +
        '<button type="button" id="upyun-dlg-copy" class="layui-btn layui-btn-sm" style="white-space:nowrap">复制</button>' +
        '</div></div></div>';

    layer.open({
        type: 1,
        title: '获取又拍云 Token',
        content: dialogHtml,
        area: function () {
            if (window.innerWidth <= 768) {
                return ['95%', '85%'];
            } else if (window.innerWidth <= 1024) {
                return ['80%', '70%'];
            } else {
                return ['50%', '50%'];
            }
        }(),
        btn: ['关闭'],
        success: function (layero) {
            layero.find('#upyun-dlg-fetch').on('click', function () {
                var username = layero.find('#upyun-dlg-username').val().trim();
                var password = layero.find('#upyun-dlg-password').val().trim();
                if (!username || !password) {
                    layer.msg('请输入用户名和密码', {icon: 2, time: 2000});
                    return;
                }
                var loadIdx = layer.load(2);
                $.ajax({
                    type: 'POST',
                    url: '/api/dcdn/upyun/token',
                    contentType: 'application/json',
                    dataType: 'json',
                    data: JSON.stringify({username: username, password: password}),
                    success: function (res) {
                        layer.close(loadIdx);
                        if (res.status) {
                            layero.find('#upyun-dlg-token').val(res.data);
                            layer.msg('Token 获取成功', {icon: 1, time: 2000});
                        } else {
                            layer.msg(res.msg || '获取失败，请检查账号密码', {icon: 2, time: 3000});
                        }
                    },
                    error: function () {
                        layer.close(loadIdx);
                        layer.msg('请求失败，请重试', {icon: 2, time: 2500});
                    }
                });
            });
            layero.find('#upyun-dlg-copy').on('click', function () {
                var token = layero.find('#upyun-dlg-token').val();
                if (!token) {
                    layer.msg('暂无 Token 可复制', {icon: 2, time: 1500});
                    return;
                }
                if (navigator.clipboard) {
                    navigator.clipboard.writeText(token).then(function () {
                        layer.msg('已复制到剪贴板', {icon: 1, time: 1500});
                    });
                } else {
                    layero.find('#upyun-dlg-token')[0].select();
                    document.execCommand('copy');
                    layer.msg('已复制到剪贴板', {icon: 1, time: 1500});
                }
            });
        }
    });
}
