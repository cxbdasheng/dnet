// 域名验证正则表达式
const DOMAIN_REGEX = /^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$/;
const ipv4Regex = /^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/;
// IPv6 完整格式检查
const ipv6FullRegex = /^([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}$/;
// IPv6 压缩格式检查
const ipv6CompressedRegex = /^(([0-9a-fA-F]{1,4}:){1,7}:|:((:[0-9a-fA-F]{1,4}){1,7}|:))$/;
// IPv6 混合格式检查（包含 ::）
const ipv6MixedRegex = /^((([0-9a-fA-F]{1,4}:){1,6}:)|(::))([0-9a-fA-F]{1,4}:){0,5}[0-9a-fA-F]{1,4}$|^::$/;

// 域名验证函数
function isValidDomain(domain) {
    var domainToCheck = domain;
    if (domain.startsWith('*.')) {
        domainToCheck = domain.substring(2); // 去掉 '*.' 前缀
        if (!domainToCheck) {
            return false; // 泛解析域名格式错误
        }
    }
    return DOMAIN_REGEX.test(domainToCheck) && domainToCheck.indexOf('.') !== -1;
}

// 获取配置显示文本
function getConfigDisplayText(config) {
    if (config.name) {
        return config.name;
    } else if (config.domain) {
        let displayText = config.id + ' - ' + config.domain;
        // 如果有多记录类型，显示类型列表
        if (config.types && Array.isArray(config.types) && config.types.length > 0) {
            displayText += ' (' + config.types.join(', ') + ')';
        } else if (config.type) {
            // 旧格式兼容
            displayText += ' (' + config.type + ')';
        }
        return displayText;
    }
    return config.id;
}

// 遍历表单中所有带 lay-verify 的字段，逐一校验，返回是否全部通过
function validateLayuiForm(form, layer, $) {
    let isValid = true;
    $('.layui-form').find('input[lay-verify], select[lay-verify], textarea[lay-verify]').each(function () {
        const $this = $(this);
        const verifyType = $this.attr('lay-verify');
        if (verifyType) {
            const value = $this.val();
            const verifyRules = verifyType.split('|');
            for (let i = 0; i < verifyRules.length; i++) {
                const rule = verifyRules[i];
                if (form.config.verify[rule]) {
                    const result = form.config.verify[rule](value, this);
                    if (result) {
                        layer.msg(result);
                        $this.focus();
                        isValid = false;
                        return false; // 跳出循环
                    }
                }
            }
        }
        if (!isValid) return false; // 跳出 each 循环
    });
    return isValid;
}