const tokenConf = {
    defaultToken: 'source',
    ignoreCase: true,
    brackets: [{open: '{', close: '}', token: 'delimiter.bracket'}],
    tokenizer: {
        root: [
            [/(")/, 'delimiter.bracket'],
            [/[;,.]/, 'delimiter'],
            [/\\.* |~|~\*|!~|!~\*/, 'string.regexp'],
            [/\b\d+\w+\b/, 'number'],
            [/\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d{1,5})?\b/, 'number'],
            [/\b(ip_hash|upstream|server)\b/, 'http.upstream'],
            [/\b(add_header|expires|server_tokens|autoindex|autoindex_exact_size|autoindex_localtime)\b/, 'http.headers'],
            [/\b(map|map_hash_max_size|map_hash_bucket_size)\b/, 'module.http'],
            [/\b(http|stream)\b/, 'module.http'],
            [
                /\b(gzip|gzip_buffers|gzip_comp_level|gzip_disable|gzip_http.version|gzip_min_length|gzip_proxied|gzip_types|gzip_vary)\b/,
                'module.http',
            ],
            [/\s(on|off)\b/, 'module.main'],
            [/\b(access_log|log_format)\s/, 'module.log'],
            [
                /\b(proxy_buffer_size|proxy_buffering|proxy_buffers|proxy_busy_buffers_size|proxy_cache|proxy_cache_background_update|proxy_cache_bypass|proxy_cache_convert_head|proxy_cache_key|proxy_cache_lock|proxy_cache_lock_age|proxy_cache_lock_timeout|proxy_cache_max_range_offset|proxy_cache_methods|proxy_cache_min_uses|proxy_cache_path|proxy_cache_purge|proxy_cache_revalidate|proxy_cache_use_stale|proxy_cache_valid|proxy_connect_timeout|proxy_headers_hash_bucket_size|proxy_headers_hash_max_size|proxy_hide_header|proxy_http_version|proxy_ignore_client_abort|proxy_intercept_errors|proxy_max_temp_file_size|proxy_method|proxy_next_upstream|proxy_next_upstream_tries|proxy_next_upstream_timeout|proxy_pass|proxy_pass_header|proxy_pass_request_body|proxy_pass_request_headers|proxy_read_timeout|proxy_redirect|proxy_redirect_errors|proxy_send_lowat|proxy_send_timeout|proxy_set_body|proxy_set_header|proxy_store|proxy_store_access|proxy_temp_file_write_size|proxy_t|emp_pathproxy_upstream_fail_timeout|proxy_upstream_max_fails|proxy_temp_path|proxy_ssl_certificate|proxy_ssl_certificate_key|proxy_ssl_protocols|proxy_ssl_ciphers|proxy_ssl_verify|proxy_ssl_trusted_certificate|proxy_ssl_verify_depth|proxy_ssl_session_reuse|rewrite|return|break)\b/,
                'http.proxy',
            ],
            [
                /\b(ssl|ssl_buffer_size|ssl_certificate|ssl_certificate_key|ssl_ciphers|ssl_client_certificate|ssl_crl|ssl_dhparam|ssl_ecdh_curve|ssl_password_file|ssl_prefer_server_ciphers|ssl_protocols|ssl_session_cache|ssl_session_ticket_key|ssl_session_tickets|ssl_session_timeout|ssl_stapling|ssl_stapling_file|ssl_stapling_responder|ssl_stapling_verify|ssl_trusted_certificate|ssl_verify_client|ssl_verify_depth)\b/,
                'module.http',
            ],
            [
                /\b(daemon|env|debug_points|error_log|log_not_found|include|lock_file|master_process|pid|ssl_engine|timer_resolution|user|worker_cpu_affinity|worker_priority|worker_processes|worker_rlimit_core|worker_rlimit_nofile|worker_rlimit_sigpending|working_directory|try_files)\b/,
                'module.main',
            ],
            [
                /\b(index|alias|chunked_transfer_encoding|client_body_in_file_only|client_body_buffer_size|client_body_temp_path|client_body_timeout|client_header_buffer_size|client_header_timeout|types_hash_max_size|client_max_body_size|default_type|error_page|index |internal|keepalive_timeout|keepalive_requests|large_client_header_buffers|limit_except|limit_rate|listen|location|msie_padding|msie_refresh|optimize_server_names|port_in_redirect|recursive_error_pages|reset_timedout_connection|resolver|resolver_timeout|root|satisfy_any|send_timeout|sendfile|sendfile_max_chunk|server|server_name|server_names_hash_max_size|server_names_hash_bucket_size|tcp_nodelay|tcp_nopush|types |try_files|charset|stub_status|allow|deny)\s/,
                'module.http',
            ],
            [
                /\b(accept_mutex|accept_mutex_delay|debug_connection|devpoll_changes|devpoll_events|epoll_events|kqueue_changes|kqueue_events|multi_accept|rtsig_signo|rtsig_overflow_events|rtsig_overflow_test|rtsig_overflow_threshold|use|worker_connections)\b/,
                'module.events',
            ],
            [/\b(add_before_body|add_after_body|addition_types)\b/, 'module.http.addition'],
            [/\b(events)\b/, 'module.events'],
            [
                /\b(fastcgi_index|fastcgi_hide_header|fastcgi_ignore_client_abort|fastcgi_intercept_errors|fastcgi_param|fastcgi_pass|fastcgi_pass_header|fastcgi_read_timeout|fastcgi_redirect_errors|fa|stcgi_storefastcgi_store_access|fastcgi_buffers|fastcgi_buffers_size|fastcgi_temp_path|fastcgi_buffer_size|fastcgi_connect_timeout|fastcgi_send_timeout|fastcgi_split_path_info|fastcgi_busy_buffers_size|fastcgi_temp_file_write_size)\b/,
                'module.http',
            ],
            [/\b(if)\b/, 'module.condition'],
            [/\$\w+/, 'variable'],
            [/#.*$/, 'comment'],
        ],
        comment: [[/#.*$/, 'comment']],
        numbers: [
            ['-?(\\d*\\.)?\\d+([eE][\\-+]?\\d+)?', {token: 'attribute.value.number', next: '@units'}],
            ['#[0-9a-fA-F_]+(?!\\w)', 'attribute.value.hex'],
        ],
        units: [['(M)?', 'attribute.value.unit', '@pop']],
    },
};
const themeConfig = {
    colors: {
        // 'attribute.value.unit': '#68217a'
    },
    base: 'vs-dark',
    inherit: true,
    rules: [
        {
            token: 'module.http',
            foreground: '#00bbbb',
        },
        {
            token: 'module.events',
            foreground: '#00bbbb',
        },
        {
            token: 'http.headers',
            foreground: '#00bbbb',
        },
        {
            token: 'http.proxy',
            foreground: '#58f18e',
        },
        {
            token: 'module.main',
            foreground: '#c152e4',
            fontStyle: 'bold',
        },
        {
            token: 'module.log',
            foreground: '#dd9970',
        },
        {
            token: 'module.condition',
            foreground: '#b038c4',
        },
        {
            token: 'module.http.addition',
            foreground: '#c152e4',
        },
        {
            token: 'keywords',
            foreground: '#9effff',
            fontStyle: 'bold',
        },
        {
            token: 'http.upstream',
            foreground: '#0078d0',
            fontStyle: 'bold',
        },
        {
            token: 'identifier',
            foreground: '#8e44ad',
        },
        {
            token: 'delimiter.bracket',
            foreground: '#d9d9d9',
        },
        {
            token: 'delimiter',
            foreground: '#d9d9d9',
        },
    ],
};

let editor,
    defaultLang = 'nginx',
    defaultTheme = 'vs-dark';

require.config({
    paths: {
        vs: 'https://cdn.jsdelivr.net/npm/monaco-editor@0.31.1/min/vs'
    },
    'vs/nls': {availableLanguages: {'*': 'zh-cn'}}
});

const processResponse = (data, redirect = "/admin/sites", successMessage) => {
    if (data.message === 'OK') {
        if (redirect) {
            window.location = redirect;
        }
        $("#successMessage").html(successMessage);
        $("#alertSuccess").show()
    } else {
        $("#message").html(data.message);
        $("#alert").show();
        $("#alertSuccess").hide();
    }
}

$('#enableSSL').click(() => {
    $("#alert").hide();
    $("#alertSuccess").hide();

    // Show spinner, hide icon, update status
    $('#ssl_spinner').removeClass('hidden');
    $('#ssl_icon').addClass('hidden');
    $('#ssl_status').text("处理中...");
    $('#enableSSL').attr("disabled", true);

    const domains = $("#domains").val().split(",");
    const configName = $("#filename").val();
    const proxy = $("#proxy").val();

    $.get('/admin/ssl/renew', {domains, configName}, (data) => {
        processResponse(data, false, "SSL 签名成功,自动添加 SSL 部分");

        // Restore button state
        $('#ssl_spinner').addClass('hidden');
        $('#ssl_icon').removeClass('hidden');
        $('#ssl_status').text("Let's Encrypt");
        $('#enableSSL').attr("disabled", false);

        if (data.message === 'OK') {
            $.get('/admin/sites/template', {domains, ssl: true, proxy, configName}, (data) => {
                editor.getModel().setValue(data.content);
            });
        }
    }).fail(function () {
        // In case of error, restore the button
        $('#ssl_spinner').addClass('hidden');
        $('#ssl_icon').removeClass('hidden');
        $('#ssl_status').text("Let's Encrypt");
        $('#enableSSL').attr("disabled", false);
        $("#message").html("证书请求失败，请检查网络连接或查看日志");
        $("#alert").show();
    });
});

$('#getTemplate').click(() => {
    const domains = $("#domains").val() ? $("#domains").val().split(",") : ["localhost"];
    const proxy = $("#proxy").val() ? $("#proxy").val() : "http://localhost";
    $.get('/admin/sites/template', {domains, proxy}, (data) => {
        editor.getModel().setValue(data.content);
    });
});

// 保存配置文件
$('#saveSitesConf').click(() => {
    let json = {
        filename: $("#filename").val(),
        content: editor.getValue()
    };

    if ($("#domains").val() || $("#proxy").val()) {
        json = {
            ...json,
            domains: $("#domains").val().split(","),
            proxy: $("#proxy").val(),
        }
    }

    $.post('/admin/sites/save', json, (data) => {
        processResponse(data);
    });
});

$('#saveNginxConf').click(() => {
    $.post('/admin/nginx/save', {
        action: "saveConfig",
        name: $("#filename").val(),
        content: editor.getValue()
    }, (data) => {
        processResponse(data, "/");
    });
});

$('#btnFormatterNginxConf').click(() => {
    editor.trigger('a', 'editor.action.formatDocument')
});

/**
 * 格式化 nginx
 * @param text
 * @returns {*}
 */
const nginxFormatter = (text) => {
    const indent = "    ";
    modifyOptions({INDENTATION: indent});
    let cleanLines = clean_lines(text);
    modifyOptions({trailingBlankLines: false});
    cleanLines = join_opening_bracket(cleanLines);
    cleanLines = perform_indentation(cleanLines, indent);
    return cleanLines.join("\n");
}