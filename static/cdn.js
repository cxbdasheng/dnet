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
        idHelpHtml: "<a target='_blank' href='https://console.upyun.com/account/operator/'>操作员管理（生成 Token）</a>",
        typeHelpHtml: "<a target='_blank' href='https://console.upyun.com/cdn/list/'>又拍云 CDN 控制台</a>",
        maxSources: [1],
        protocolTipHtml: [
            "<tip></tip>",
        ]
    },
}
