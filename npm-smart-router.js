// npm-smart-router-stream.js
// EdgeOne边缘函数路由

async function handleEvent(event) {
    const { request } = event;
    const url = new URL(request.url);
    const rawPathname = url.pathname;
    const pathname = decodeURIComponent(rawPathname);
    const method = request.method;
    
    console.log(`[EdgeOne] ${method} ${pathname}`);
    
    // 1. 健康检查端点
    if (pathname === '/_health') {
        return new Response(JSON.stringify({
            status: 'ok',
            service: 'npm-smart-mirror',
            timestamp: new Date().toISOString(),
            version: '2.0.0'
        }), {
            status: 200,
            headers: {
                'Content-Type': 'application/json',
                'Cache-Control': 'no-store, no-cache, must-revalidate',
                'Pragma': 'no-cache',
                'Access-Control-Allow-Origin': '*'
            }
        });
    }
    
    // 2. 处理OPTIONS请求
    if (method === 'OPTIONS') {
        return new Response(null, {
            status: 204,
            headers: {
                'Access-Control-Allow-Origin': '*',
                'Access-Control-Allow-Methods': 'GET, HEAD, OPTIONS, POST',
                'Access-Control-Allow-Headers': 'Accept, Accept-Encoding, Content-Type, Authorization',
                'Access-Control-Max-Age': '86400',
                'Cache-Control': 'no-store'
            }
        });
    }
    
    // 3. 解析npm包名
    const packageName = extractPackageName(pathname);
    
    if (packageName) {
        console.log(`[npm包] 检测到包名: ${packageName}`);
        
        // 检查包是否在COS中
        const inCOS = await checkPackageInCOS(packageName);
        
        if (inCOS) {
            console.log(`[路由] ${packageName} -> COS后端`);
            return await proxyToBackendStream(rawPathname, request);
        } else {
            const redirectUrl = `https://registry.npmmirror.com${rawPathname}`;
            console.log(`[路由] ${packageName} -> 302 ${redirectUrl}`);
            return new Response(null, {
                status: 302,
                headers: {
                    'Location': redirectUrl,
                    'Access-Control-Allow-Origin': '*',
                    'Cache-Control': 'no-store'
                }
            });
        }
    }
    
    // 4. 其他所有请求转发到源站
    console.log(`[其他] 转发到源站: ${pathname}`);
    return forwardToOriginStream(request);
}

// 转发到源站 - 流式传输
async function forwardToOriginStream(request) {
    const url = new URL(request.url);
    const originUrl = `http://43.167.220.164:7634${url.pathname}${url.search}`;
    
    console.log(`[源站] 转发到: ${originUrl}`);
    
    try {
        const headers = new Headers(request.headers);
        headers.delete('Accept-Encoding');
        headers.set('Accept-Encoding', 'identity');
        
        const response = await fetch(originUrl, {
            method: request.method,
            headers: headers,
            body: request.method !== 'GET' && request.method !== 'HEAD' ? request.body : undefined
        });
        
        // 流式传输响应
        return new Response(response.body, {
            status: response.status,
            headers: response.headers
        });
        
    } catch (error) {
        console.error(`[源站] 请求失败: ${error.message}`);
        
        return new Response(JSON.stringify({
            error: 'Origin service unavailable',
            message: error.message,
            timestamp: new Date().toISOString()
        }), {
            status: 502,
            headers: {
                'Content-Type': 'application/json',
                'Cache-Control': 'no-store',
                'Access-Control-Allow-Origin': '*'
            }
        });
    }
}

// 提取包名
function extractPackageName(pathname) {
    // 排除已知的非npm路径
    if (pathname === '/' || 
        pathname === '' || 
        pathname.startsWith('/api/') ||
        pathname.startsWith('/assets/') ||
        pathname.startsWith('/download/') ||
        pathname === '/favicon.ico' ||
        pathname === '/koishi.png' ||
        pathname === '/index.json' ||
        pathname.startsWith('/-/') ||
        pathname.startsWith('/packages') ||
        pathname.startsWith('/package/') ||
        pathname.startsWith('/stats')) {
        return null;
    }
    
    // 处理tarball路径（.tgz文件）
    if (pathname.includes('/-/') && pathname.endsWith('.tgz')) {
        const match = pathname.match(/^\/([^/]+)\/-\/[^/]+\.tgz$/);
        const scopedMatch = pathname.match(/^\/(@[^/]+\/[^/]+)\/-\/[^/]+\.tgz$/);
        return match ? match[1] : (scopedMatch ? scopedMatch[1] : null);
    }
    
    // 处理包元数据路径
    const parts = pathname.split('/').filter(p => p.length > 0);
    if (parts.length === 0) return null;
    
    // 如果是作用域包
    if (parts[0].startsWith('@')) {
        return parts.length >= 2 ? `${parts[0]}/${parts[1]}` : null;
    }
    
    // 普通包
    return parts[0];
}

// 检查包是否在COS中
async function checkPackageInCOS(packageName) {
    // 已知在COS中的包
    const knownCOSPackages = [
        'koishi-plugin-vrchat-status',
        'koishi-plugin-dataview'
    ];
    
    // 首先检查已知包列表
    if (knownCOSPackages.includes(packageName)) {
        console.log(`[检查] ${packageName} 在已知COS包列表中`);
        return true;
    }
    
    try {
        console.log(`[检查] 检查包 ${packageName} 是否在COS中`);
        
        const response = await fetch('http://43.167.220.164:7634/api/packages', {
            headers: { 
                'Accept': 'application/json',
                'Accept-Encoding': 'identity'
            }
        });
        
        if (!response.ok) {
            console.log(`[检查] 后端API不可用: ${response.status}`);
            return false;
        }
        
        const data = await response.json();
        const found = data.packages.some(pkg => 
            pkg.name === packageName && pkg.syncStatus === 'success'
        );
        
        console.log(`[检查] ${packageName} 在COS中: ${found}`);
        return found;
        
    } catch (error) {
        console.error(`[检查] 检查失败: ${error.message}`);
        return false;
    }
}

// 代理到后端（COS包）- 流式传输
async function proxyToBackendStream(pathname, request) {
    const backendUrl = `http://43.167.220.164:7634${pathname}`;
    
    console.log(`[COS] 转发到后端: ${backendUrl}`);
    
    try {
        const headers = new Headers(request.headers);
        headers.delete('Accept-Encoding');
        headers.set('Accept-Encoding', 'identity');
        
        const response = await fetch(backendUrl, {
            method: request.method,
            headers: headers,
            body: request.method !== 'GET' && request.method !== 'HEAD' ? request.body : undefined
        });
        
        // 流式传输响应
        return new Response(response.body, {
            status: response.status,
            headers: response.headers
        });
        
    } catch (error) {
        console.error(`[COS] 请求失败: ${error.message}`);
        return new Response(JSON.stringify({
            error: 'COS backend service error',
            message: error.message
        }), {
            status: 502,
            headers: {
                'Content-Type': 'application/json',
                'Cache-Control': 'no-store',
                'Access-Control-Allow-Origin': '*'
            }
        });
    }
}

// 代理到npmjs - 流式传输
async function proxyToNpmjsStream(pathname, request) {
    const npmUrl = `https://registry.npmjs.org${pathname}`;
    
    console.log(`[npmjs] 转发到: ${npmUrl}`);
    
    try {
        const headers = new Headers(request.headers);
        headers.set('Host', 'registry.npmjs.org');
        const isTarball = pathname.includes('/-/') && pathname.endsWith('.tgz');
        headers.delete('Accept-Encoding');
        headers.set('Accept-Encoding', 'identity');
        if (!isTarball) {
            headers.set('Accept', 'application/json');
        }
        
        const response = await fetch(npmUrl, {
            method: request.method,
            headers: headers,
            body: request.method !== 'GET' && request.method !== 'HEAD' ? request.body : undefined
        });
        
        // 创建新的响应头
        const respHeaders = new Headers(response.headers);
        respHeaders.set('X-Package-Source', 'npmjs');
        respHeaders.set('X-Route-Type', 'NPMJS-PROXY');
        respHeaders.set('Access-Control-Allow-Origin', '*');
        
        // 流式传输响应
        return new Response(response.body, {
            status: response.status,
            headers: respHeaders
        });
        
    } catch (error) {
        console.error(`[npmjs] 请求失败: ${error.message}`);
        return new Response(JSON.stringify({
            error: 'npmjs.org proxy error',
            message: error.message
        }), {
            status: 502,
            headers: {
                'Content-Type': 'application/json',
                'Cache-Control': 'no-store',
                'Access-Control-Allow-Origin': '*'
            }
        });
    }
}

// EdgeOne入口
addEventListener('fetch', event => {
    event.respondWith(handleEvent(event));
});
