'use strict';

/**
 * @ngdoc overview
 * @name yapp
 * @description
 * # yapp
 *
 * Main module of the application.
 */
var apiUrl = "http://127.0.0.1:1270";

(function (ElementProto) {
    if (typeof ElementProto.matches !== 'function') {
        ElementProto.matches = ElementProto.msMatchesSelector || ElementProto.mozMatchesSelector || ElementProto.webkitMatchesSelector || function matches(selector) {
            var element = this;
            var elements = (element.document || element.ownerDocument).querySelectorAll(selector);
            var index = 0;

            while (elements[index] && elements[index] !== element) {
                ++index;
            }

            return Boolean(elements[index]);
        };
    }

    if (typeof ElementProto.closest !== 'function') {
        ElementProto.closest = function closest(selector) {
            var element = this;

            while (element && element.nodeType === 1) {
                if (element.matches(selector)) {
                    return element;
                }

                element = element.parentNode;
            }

            return null;
        };
    }
})(window.Element.prototype);

function transformReq(obj) {
    var str = [];
    for(var p in obj)
    str.push(encodeURIComponent(p) + "=" + encodeURIComponent(obj[p]));
    return str.join("&");
}

angular
  .module('tsapp', [
    'ui.router',
    'ngAnimate'
  ])
  .config(function($stateProvider, $urlRouterProvider) {

    $urlRouterProvider.otherwise('/settings');

    $stateProvider
        .state('settings', {
          url: '/settings',
          templateUrl: 'views/settings.html',
          controller: 'SettingsCtrl'
        })
        .state('about', {
          url: '/about',
          templateUrl: 'views/about.html'
        });
  })
  .controller('SettingsCtrl', ['$scope', '$http', function($scope, $http) {
    $scope.config = {};
    $scope.shadowsocks = [];
    function reqSS(url, method, data, errDom){
        var params = {
            method: method,
            url: url,
        }
        if (data) {
            params.data = data;
        }
        if (method != 'GET') {
            params.transformRequest = transformReq;
            params.headers = {'Content-Type': 'application/x-www-form-urlencoded'}
        }
        return $http(params).then(
            function(res){
                console.info(res.data)
                if (res.data.ok) {
                    $scope.shadowsocks = res.data.data;
                } else if (errDom) {
                    errDom.innerText = res.data.message;
                    errDom.className = errDom.className.split('hide').join(' ')
                }
            },
            function(res){}
        );
    }
    reqSS(apiUrl + '/shadowsocks', "GET")
    $scope.ssAction = {
        add: function($event){
            var tr = ($event.currentTarget || $event.srcElement).closest('tr')
            var ipt = tr.querySelectorAll('input')[0];
            var errE = tr.querySelectorAll('.error')[0];
            var nss = ipt.value;
            reqSS(apiUrl + '/shadowsocks', "POST", {ss: nss}, errE).then(function(res){
                if (!errE.innerText) {
                    ipt.value = "";
                }
            })
        },
        save: function($event, ss){
            var elem = $event.currentTarget || $event.srcElement
            var nss = elem.closest('tr').querySelectorAll('input')[0].value;
            var tr = elem.closest('tr');
            var errE = tr.querySelectorAll('.error')[0];
            console.info("new ss is", nss)
            if (nss==ss) {
                return this.cancel($event);
            }
            reqSS(apiUrl + '/shadowsocks?ss='+encodeURIComponent(ss), "PUT", {ss: nss}, errE)
        },
        delete: function(ss){
            reqSS(apiUrl + '/shadowsocks?ss='+encodeURIComponent(ss), "DELETE")
        },
        cancel: function($event){
            var elem = $event.currentTarget || $event.srcElement
            var tr = elem.closest('tr');
            tr.className = tr.className.split('editing').join(' ');
        },
        edit: function($event){
            var elem = $event.currentTarget || $event.srcElement
            var trs = elem.closest('table').querySelectorAll('tr.shadowsocks');
            var tr = elem.closest('tr');
            tr.className += ' editing';
            var errE = tr.querySelectorAll('.error')[0];
            errE.innerText="";
            errE.className +=" hide";
        }
    }
    function set(name, value){
        var url = apiUrl + "/set";
        var params = {name: name, value: value};
        $http({
            method: "POST",
            url: url,
            data: params,
            transformRequest: transformReq,
            headers: {'Content-Type': 'application/x-www-form-urlencoded'}
        }).then(
            function(res){
                console.info(res.data)
                if (res.data.data) {
                    $scope.config = res.data.data;
                }
            },
            function(res){}
        )       
    }
    $scope.setDiyDomains = function(){
        var dd = document.getElementsByName('diy_domains')[0]
        set("diy_domains", dd.value)
    }
    $scope.toggle = function(name){
        var ele = document.getElementsByName(name)[0]
        var value = ele.checked?'on':'off';
        set(name, value);
    }
    $http({
        method: "GET",
        url: apiUrl + "/settings"
    }).then(
        function(res){
            console.info(res.data)
            if (res.data.data) {
                $scope.config = res.data.data;
            }
        },
        function(res){}
    ); 
  }])
  ;
