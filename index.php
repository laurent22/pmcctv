<?php

define('IS_PMCCTV_SERVER', true);
ini_set('display_errors', 1);
ini_set('display_startup_errors', 1);
error_reporting(E_ALL);

// -------------------------------------------------------------------------
// Configuration and setup
// -------------------------------------------------------------------------

$configPath =  dirname(__FILE__) . '/config.php';
$config = null;
if (file_exists($configPath)) {
	$config = require $configPath;
}

session_start();

if (!$config) {
	unset($_SESSION['logged_in']);
} else {
	if (!isset($config['thumbnailWidth'])) $config['thumbnailWidth'] = 320;
	if (!isset($config['thumbnailHeight'])) $config['thumbnailHeight'] = 240;
}

// -------------------------------------------------------------------------
// Utility functions
// -------------------------------------------------------------------------

function _t($s) { 
	return call_user_func_array('sprintf', func_get_args());
}

function pmcctv_appName() {
	return 'Server';
}

function pmcctv_getCapturedFiles($dir) {
	if (!file_exists($dir)) throw new Exception(_t('Capture directory does not exist or is not readable: %s', $dir));

	$output = array();
	$files = glob($dir . '/cap_*');
	foreach ($files as $file) {
		$basename = basename($file);
		$s = explode('_', $basename);
		$d = DateTime::createFromFormat('Ymd\THis', $s[1]);
		$output[] = array(
			'path' => $file,
			'time' => $d,
		);
	}
	return $output;
}

// -------------------------------------------------------------------------
// Handle POST requests
// -------------------------------------------------------------------------

$errorMessage = null;
$configToDisplay = null;

if ($_SERVER['REQUEST_METHOD'] == 'POST') {
	if (isset($_POST['create_config'])) {
		if (empty($_POST['captureBaseUrl'])) $errorMessage = _t('Please specify the URL to the capture directory');
		if (empty($_POST['captureDir'])) $errorMessage = _t('Please specify the path to the capture directory');
		if (empty($_POST['password'])) $errorMessage = _t('Please specify a password');
		if (empty($_POST['username'])) $errorMessage = _t('Please specify a username');

		if (!$errorMessage) {
			$configToDisplay = array();
			$configToDisplay['username'] = $_POST['username'];
			$configToDisplay['password'] = password_hash($_POST['password'], PASSWORD_DEFAULT);
			$configToDisplay['captureDir'] = $_POST['captureDir'];
			$configToDisplay['captureBaseUrl'] = $_POST['captureBaseUrl'];
		}
	}

	if (isset($_POST['login'])) {
		if ($config['username'] == $_POST['username'] && password_verify($_POST['password'], $config['password'])) {
			$_SESSION['logged_in'] = true;
		} else {
			$errorMessage = _t('Invalid username or password');
		}
	}

	if (isset($_POST['logout'])) {
		unset($_SESSION['logged_in']);
	}
}

// -------------------------------------------------------------------------
// Retrieve data to be displayed
// -------------------------------------------------------------------------

function pmcctv_friendlyDate($date) {
	return $date->format('Y-m-d');
}

function pmcctv_friendlyDateTime($date) {
	return $date->format('Y-m-d H:i:s');
}

function pmcctv_mediaType($filePath) {
	$e = strtolower(pathinfo($filePath, PATHINFO_EXTENSION));
	if ($e == 'jpg' || $e == 'png' || $e == 'jpeg') return 'image';
	return 'video';
}	

if (!isset($_SESSION['logged_in'])) $_SESSION['logged_in'] = false;

if ($config) {
	try {
		$capturedFiles = pmcctv_getCapturedFiles($config['captureDir']);
	} catch (Exception $e) {
		$errorMessage = $e->getMessage();
		$capturedFiles = array();
	}

	usort($capturedFiles, function($a, $b) {
		return $a['time']->format('Y-m-d H:i:s') < $b['time']->format('Y-m-d H:i:s');
	});

	$selectedDate = isset($_GET['date']) ? DateTime::createFromFormat('Ymd', $_GET['date']) : null;

	if (!$selectedDate && count($capturedFiles)) {
		$selectedDate = $capturedFiles[0]['time'];
	}
}

?>

<!DOCTYPE html>
<html>
<head>
<style>
/* Milligram v1.1.0 */
/* http://milligram.github.io */
html{box-sizing:border-box;font-size:62.5%}body{color:#606c76;font-family:"Roboto","Helvetica Neue","Helvetica","Arial",sans-serif;font-size:1.6em;font-weight:300;letter-spacing:.01em;line-height:1.6}*,*:after,*:before{box-sizing:inherit}blockquote{border-left:.3rem solid #d1d1d1;margin-left:0;margin-right:0;padding:1rem 1.5rem}blockquote *:last-child{margin:0}.button,button,input[type='button'],input[type='reset'],input[type='submit']{background-color:#9b4dca;border:.1rem solid #9b4dca;border-radius:.4rem;color:#fff;cursor:pointer;display:inline-block;font-size:1.1rem;font-weight:700;height:3.8rem;letter-spacing:.1rem;line-height:3.8rem;padding:0 3rem;text-align:center;text-decoration:none;text-transform:uppercase;white-space:nowrap}.button:hover,.button:focus,button:hover,button:focus,input[type='button']:hover,input[type='button']:focus,input[type='reset']:hover,input[type='reset']:focus,input[type='submit']:hover,input[type='submit']:focus{background-color:#606c76;border-color:#606c76;color:#fff;outline:0}.button.button-disabled,.button[disabled],button.button-disabled,button[disabled],input[type='button'].button-disabled,input[type='button'][disabled],input[type='reset'].button-disabled,input[type='reset'][disabled],input[type='submit'].button-disabled,input[type='submit'][disabled]{opacity:.5;cursor:default}.button.button-disabled:hover,.button.button-disabled:focus,.button[disabled]:hover,.button[disabled]:focus,button.button-disabled:hover,button.button-disabled:focus,button[disabled]:hover,button[disabled]:focus,input[type='button'].button-disabled:hover,input[type='button'].button-disabled:focus,input[type='button'][disabled]:hover,input[type='button'][disabled]:focus,input[type='reset'].button-disabled:hover,input[type='reset'].button-disabled:focus,input[type='reset'][disabled]:hover,input[type='reset'][disabled]:focus,input[type='submit'].button-disabled:hover,input[type='submit'].button-disabled:focus,input[type='submit'][disabled]:hover,input[type='submit'][disabled]:focus{background-color:#9b4dca;border-color:#9b4dca}.button.button-outline,button.button-outline,input[type='button'].button-outline,input[type='reset'].button-outline,input[type='submit'].button-outline{color:#9b4dca;background-color:transparent}.button.button-outline:hover,.button.button-outline:focus,button.button-outline:hover,button.button-outline:focus,input[type='button'].button-outline:hover,input[type='button'].button-outline:focus,input[type='reset'].button-outline:hover,input[type='reset'].button-outline:focus,input[type='submit'].button-outline:hover,input[type='submit'].button-outline:focus{color:#606c76;background-color:transparent;border-color:#606c76}.button.button-outline.button-disabled:hover,.button.button-outline.button-disabled:focus,.button.button-outline[disabled]:hover,.button.button-outline[disabled]:focus,button.button-outline.button-disabled:hover,button.button-outline.button-disabled:focus,button.button-outline[disabled]:hover,button.button-outline[disabled]:focus,input[type='button'].button-outline.button-disabled:hover,input[type='button'].button-outline.button-disabled:focus,input[type='button'].button-outline[disabled]:hover,input[type='button'].button-outline[disabled]:focus,input[type='reset'].button-outline.button-disabled:hover,input[type='reset'].button-outline.button-disabled:focus,input[type='reset'].button-outline[disabled]:hover,input[type='reset'].button-outline[disabled]:focus,input[type='submit'].button-outline.button-disabled:hover,input[type='submit'].button-outline.button-disabled:focus,input[type='submit'].button-outline[disabled]:hover,input[type='submit'].button-outline[disabled]:focus{color:#9b4dca;border-color:inherit}.button.button-clear,button.button-clear,input[type='button'].button-clear,input[type='reset'].button-clear,input[type='submit'].button-clear{color:#9b4dca;background-color:transparent;border-color:transparent}.button.button-clear:hover,.button.button-clear:focus,button.button-clear:hover,button.button-clear:focus,input[type='button'].button-clear:hover,input[type='button'].button-clear:focus,input[type='reset'].button-clear:hover,input[type='reset'].button-clear:focus,input[type='submit'].button-clear:hover,input[type='submit'].button-clear:focus{color:#606c76;background-color:transparent;border-color:transparent}.button.button-clear.button-disabled:hover,.button.button-clear.button-disabled:focus,.button.button-clear[disabled]:hover,.button.button-clear[disabled]:focus,button.button-clear.button-disabled:hover,button.button-clear.button-disabled:focus,button.button-clear[disabled]:hover,button.button-clear[disabled]:focus,input[type='button'].button-clear.button-disabled:hover,input[type='button'].button-clear.button-disabled:focus,input[type='button'].button-clear[disabled]:hover,input[type='button'].button-clear[disabled]:focus,input[type='reset'].button-clear.button-disabled:hover,input[type='reset'].button-clear.button-disabled:focus,input[type='reset'].button-clear[disabled]:hover,input[type='reset'].button-clear[disabled]:focus,input[type='submit'].button-clear.button-disabled:hover,input[type='submit'].button-clear.button-disabled:focus,input[type='submit'].button-clear[disabled]:hover,input[type='submit'].button-clear[disabled]:focus{color:#9b4dca}code{background:#f4f5f6;border-radius:.4rem;font-size:86%;padding:.2rem .5rem;margin:0 .2rem;white-space:nowrap}pre{background:#f4f5f6;border-left:.3rem solid #9b4dca;font-family:"Menlo","Consolas","Bitstream Vera Sans Mono","DejaVu Sans Mono","Monaco",monospace}pre>code{background:transparent;border-radius:0;display:block;padding:1rem 1.5rem;white-space:pre}hr{border:0;border-top:.1rem solid #f4f5f6;margin-bottom:3.5rem;margin-top:3rem}input[type='email'],input[type='number'],input[type='password'],input[type='search'],input[type='tel'],input[type='text'],input[type='url'],textarea,select{-webkit-appearance:none;-moz-appearance:none;appearance:none;background-color:transparent;border:.1rem solid #d1d1d1;border-radius:.4rem;box-shadow:none;height:3.8rem;padding:.6rem 1rem;width:100%}input[type='email']:focus,input[type='number']:focus,input[type='password']:focus,input[type='search']:focus,input[type='tel']:focus,input[type='text']:focus,input[type='url']:focus,textarea:focus,select:focus{border:.1rem solid #9b4dca;outline:0}select{padding:.6rem 3rem .6rem 1rem;background:url(data:image/svg+xml;base64,PD94bWwgdmVyc2lvbj0iMS4wIiBlbmNvZGluZz0iVVRGLTgiIHN0YW5kYWxvbmU9Im5vIj8+PHN2ZyAgIHhtbG5zOmRjPSJodHRwOi8vcHVybC5vcmcvZGMvZWxlbWVudHMvMS4xLyIgICB4bWxuczpjYz0iaHR0cDovL2NyZWF0aXZlY29tbW9ucy5vcmcvbnMjIiAgIHhtbG5zOnJkZj0iaHR0cDovL3d3dy53My5vcmcvMTk5OS8wMi8yMi1yZGYtc3ludGF4LW5zIyIgICB4bWxuczpzdmc9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIiAgIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyIgICB4bWxuczpzb2RpcG9kaT0iaHR0cDovL3NvZGlwb2RpLnNvdXJjZWZvcmdlLm5ldC9EVEQvc29kaXBvZGktMC5kdGQiICAgeG1sbnM6aW5rc2NhcGU9Imh0dHA6Ly93d3cuaW5rc2NhcGUub3JnL25hbWVzcGFjZXMvaW5rc2NhcGUiICAgZW5hYmxlLWJhY2tncm91bmQ9Im5ldyAwIDAgMjkgMTQiICAgaGVpZ2h0PSIxNHB4IiAgIGlkPSJMYXllcl8xIiAgIHZlcnNpb249IjEuMSIgICB2aWV3Qm94PSIwIDAgMjkgMTQiICAgd2lkdGg9IjI5cHgiICAgeG1sOnNwYWNlPSJwcmVzZXJ2ZSIgICBpbmtzY2FwZTp2ZXJzaW9uPSIwLjQ4LjQgcjk5MzkiICAgc29kaXBvZGk6ZG9jbmFtZT0iY2FyZXQtZ3JheS5zdmciPjxtZXRhZGF0YSAgICAgaWQ9Im1ldGFkYXRhMzAzOSI+PHJkZjpSREY+PGNjOldvcmsgICAgICAgICByZGY6YWJvdXQ9IiI+PGRjOmZvcm1hdD5pbWFnZS9zdmcreG1sPC9kYzpmb3JtYXQ+PGRjOnR5cGUgICAgICAgICAgIHJkZjpyZXNvdXJjZT0iaHR0cDovL3B1cmwub3JnL2RjL2RjbWl0eXBlL1N0aWxsSW1hZ2UiIC8+PC9jYzpXb3JrPjwvcmRmOlJERj48L21ldGFkYXRhPjxkZWZzICAgICBpZD0iZGVmczMwMzciIC8+PHNvZGlwb2RpOm5hbWVkdmlldyAgICAgcGFnZWNvbG9yPSIjZmZmZmZmIiAgICAgYm9yZGVyY29sb3I9IiM2NjY2NjYiICAgICBib3JkZXJvcGFjaXR5PSIxIiAgICAgb2JqZWN0dG9sZXJhbmNlPSIxMCIgICAgIGdyaWR0b2xlcmFuY2U9IjEwIiAgICAgZ3VpZGV0b2xlcmFuY2U9IjEwIiAgICAgaW5rc2NhcGU6cGFnZW9wYWNpdHk9IjAiICAgICBpbmtzY2FwZTpwYWdlc2hhZG93PSIyIiAgICAgaW5rc2NhcGU6d2luZG93LXdpZHRoPSI5MDMiICAgICBpbmtzY2FwZTp3aW5kb3ctaGVpZ2h0PSI1OTQiICAgICBpZD0ibmFtZWR2aWV3MzAzNSIgICAgIHNob3dncmlkPSJ0cnVlIiAgICAgaW5rc2NhcGU6em9vbT0iMTIuMTM3OTMxIiAgICAgaW5rc2NhcGU6Y3g9Ii00LjExOTMxODJlLTA4IiAgICAgaW5rc2NhcGU6Y3k9IjciICAgICBpbmtzY2FwZTp3aW5kb3cteD0iNTAyIiAgICAgaW5rc2NhcGU6d2luZG93LXk9IjMwMiIgICAgIGlua3NjYXBlOndpbmRvdy1tYXhpbWl6ZWQ9IjAiICAgICBpbmtzY2FwZTpjdXJyZW50LWxheWVyPSJMYXllcl8xIj48aW5rc2NhcGU6Z3JpZCAgICAgICB0eXBlPSJ4eWdyaWQiICAgICAgIGlkPSJncmlkMzA0MSIgLz48L3NvZGlwb2RpOm5hbWVkdmlldz48cG9seWdvbiAgICAgcG9pbnRzPSIwLjE1LDAgMTQuNSwxNC4zNSAyOC44NSwwICIgICAgIGlkPSJwb2x5Z29uMzAzMyIgICAgIHRyYW5zZm9ybT0ibWF0cml4KDAuMzU0MTEzODcsMCwwLDAuNDgzMjkxMSw5LjMyNDE1NDUsMy42MjQ5OTkyKSIgICAgIHN0eWxlPSJmaWxsOiNkMWQxZDE7ZmlsbC1vcGFjaXR5OjEiIC8+PC9zdmc+) center right no-repeat}select:focus{background-image:url(data:image/svg+xml;base64,PD94bWwgdmVyc2lvbj0iMS4wIiBlbmNvZGluZz0iVVRGLTgiIHN0YW5kYWxvbmU9Im5vIj8+PHN2ZyAgIHhtbG5zOmRjPSJodHRwOi8vcHVybC5vcmcvZGMvZWxlbWVudHMvMS4xLyIgICB4bWxuczpjYz0iaHR0cDovL2NyZWF0aXZlY29tbW9ucy5vcmcvbnMjIiAgIHhtbG5zOnJkZj0iaHR0cDovL3d3dy53My5vcmcvMTk5OS8wMi8yMi1yZGYtc3ludGF4LW5zIyIgICB4bWxuczpzdmc9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIiAgIHhtbG5zPSJodHRwOi8vd3d3LnczLm9yZy8yMDAwL3N2ZyIgICB4bWxuczpzb2RpcG9kaT0iaHR0cDovL3NvZGlwb2RpLnNvdXJjZWZvcmdlLm5ldC9EVEQvc29kaXBvZGktMC5kdGQiICAgeG1sbnM6aW5rc2NhcGU9Imh0dHA6Ly93d3cuaW5rc2NhcGUub3JnL25hbWVzcGFjZXMvaW5rc2NhcGUiICAgZW5hYmxlLWJhY2tncm91bmQ9Im5ldyAwIDAgMjkgMTQiICAgaGVpZ2h0PSIxNHB4IiAgIGlkPSJMYXllcl8xIiAgIHZlcnNpb249IjEuMSIgICB2aWV3Qm94PSIwIDAgMjkgMTQiICAgd2lkdGg9IjI5cHgiICAgeG1sOnNwYWNlPSJwcmVzZXJ2ZSIgICBpbmtzY2FwZTp2ZXJzaW9uPSIwLjQ4LjQgcjk5MzkiICAgc29kaXBvZGk6ZG9jbmFtZT0iY2FyZXQuc3ZnIj48bWV0YWRhdGEgICAgIGlkPSJtZXRhZGF0YTMwMzkiPjxyZGY6UkRGPjxjYzpXb3JrICAgICAgICAgcmRmOmFib3V0PSIiPjxkYzpmb3JtYXQ+aW1hZ2Uvc3ZnK3htbDwvZGM6Zm9ybWF0PjxkYzp0eXBlICAgICAgICAgICByZGY6cmVzb3VyY2U9Imh0dHA6Ly9wdXJsLm9yZy9kYy9kY21pdHlwZS9TdGlsbEltYWdlIiAvPjwvY2M6V29yaz48L3JkZjpSREY+PC9tZXRhZGF0YT48ZGVmcyAgICAgaWQ9ImRlZnMzMDM3IiAvPjxzb2RpcG9kaTpuYW1lZHZpZXcgICAgIHBhZ2Vjb2xvcj0iI2ZmZmZmZiIgICAgIGJvcmRlcmNvbG9yPSIjNjY2NjY2IiAgICAgYm9yZGVyb3BhY2l0eT0iMSIgICAgIG9iamVjdHRvbGVyYW5jZT0iMTAiICAgICBncmlkdG9sZXJhbmNlPSIxMCIgICAgIGd1aWRldG9sZXJhbmNlPSIxMCIgICAgIGlua3NjYXBlOnBhZ2VvcGFjaXR5PSIwIiAgICAgaW5rc2NhcGU6cGFnZXNoYWRvdz0iMiIgICAgIGlua3NjYXBlOndpbmRvdy13aWR0aD0iOTAzIiAgICAgaW5rc2NhcGU6d2luZG93LWhlaWdodD0iNTk0IiAgICAgaWQ9Im5hbWVkdmlldzMwMzUiICAgICBzaG93Z3JpZD0idHJ1ZSIgICAgIGlua3NjYXBlOnpvb209IjEyLjEzNzkzMSIgICAgIGlua3NjYXBlOmN4PSItNC4xMTkzMTgyZS0wOCIgICAgIGlua3NjYXBlOmN5PSI3IiAgICAgaW5rc2NhcGU6d2luZG93LXg9IjUwMiIgICAgIGlua3NjYXBlOndpbmRvdy15PSIzMDIiICAgICBpbmtzY2FwZTp3aW5kb3ctbWF4aW1pemVkPSIwIiAgICAgaW5rc2NhcGU6Y3VycmVudC1sYXllcj0iTGF5ZXJfMSI+PGlua3NjYXBlOmdyaWQgICAgICAgdHlwZT0ieHlncmlkIiAgICAgICBpZD0iZ3JpZDMwNDEiIC8+PC9zb2RpcG9kaTpuYW1lZHZpZXc+PHBvbHlnb24gICAgIHBvaW50cz0iMjguODUsMCAwLjE1LDAgMTQuNSwxNC4zNSAiICAgICBpZD0icG9seWdvbjMwMzMiICAgICB0cmFuc2Zvcm09Im1hdHJpeCgwLjM1NDExMzg3LDAsMCwwLjQ4MzI5MTEsOS4zMjQxNTUzLDMuNjI1KSIgICAgIHN0eWxlPSJmaWxsOiM5YjRkY2Y7ZmlsbC1vcGFjaXR5OjEiIC8+PC9zdmc+)}textarea{padding-bottom:.6rem;padding-top:.6rem;min-height:6.5rem}label,legend{font-size:1.6rem;font-weight:700;display:block;margin-bottom:.5rem}fieldset{border-width:0;padding:0}input[type='checkbox'],input[type='radio']{display:inline}.label-inline{font-weight:normal;display:inline-block;margin-left:.5rem}.container{margin:0 auto;max-width:112rem;padding:0 2rem;position:relative;width:100%}.row{display:flex;flex-direction:column;padding:0;width:100%}.row .row-wrap{flex-wrap:wrap}.row .row-no-padding{padding:0}.row .row-no-padding>.column{padding:0}.row .row-top{align-items:flex-start}.row .row-bottom{align-items:flex-end}.row .row-center{align-items:center}.row .row-stretch{align-items:stretch}.row .row-baseline{align-items:baseline}.row .column{display:block;flex:1;margin-left:0;max-width:100%;width:100%}.row .column .col-top{align-self:flex-start}.row .column .col-bottom{align-self:flex-end}.row .column .col-center{align-self:center}.row .column.column-offset-10{margin-left:10%}.row .column.column-offset-20{margin-left:20%}.row .column.column-offset-25{margin-left:25%}.row .column.column-offset-33,.row .column.column-offset-34{margin-left:33.3333%}.row .column.column-offset-50{margin-left:50%}.row .column.column-offset-66,.row .column.column-offset-67{margin-left:66.6666%}.row .column.column-offset-75{margin-left:75%}.row .column.column-offset-80{margin-left:80%}.row .column.column-offset-90{margin-left:90%}.row .column.column-10{flex:0 0 10%;max-width:10%}.row .column.column-20{flex:0 0 20%;max-width:20%}.row .column.column-25{flex:0 0 25%;max-width:25%}.row .column.column-33,.row .column.column-34{flex:0 0 33.3333%;max-width:33.3333%}.row .column.column-40{flex:0 0 40%;max-width:40%}.row .column.column-50{flex:0 0 50%;max-width:50%}.row .column.column-60{flex:0 0 60%;max-width:60%}.row .column.column-66,.row .column.column-67{flex:0 0 66.6666%;max-width:66.6666%}.row .column.column-75{flex:0 0 75%;max-width:75%}.row .column.column-80{flex:0 0 80%;max-width:80%}.row .column.column-90{flex:0 0 90%;max-width:90%}@media (min-width: 40rem){.row{flex-direction:row;margin-left:-1rem;width:calc(100% + 2.0rem)}.row .column{margin-bottom:inherit;padding:0 1rem}}a{color:#9b4dca;text-decoration:none}a:hover{color:#606c76}dl,ol,ul{margin-top:0;padding-left:0}dl ul,dl ol,ol ul,ol ol,ul ul,ul ol{font-size:90%;margin:1.5rem 0 1.5rem 3rem}dl{list-style:none}ul{list-style:circle inside}ol{list-style:decimal inside}dt,dd,li{margin-bottom:1rem}.button,button{margin-bottom:1rem}input,textarea,select,fieldset{margin-bottom:1.5rem}pre,blockquote,dl,figure,table,p,ul,ol,form{margin-bottom:2.5rem}table{width:100%}th,td{border-bottom:.1rem solid #e1e1e1;padding:1.2rem 1.5rem;text-align:left}th:first-child,td:first-child{padding-left:0}th:last-child,td:last-child{padding-right:0}p{margin-top:0}h1,h2,h3,h4,h5,h6{font-weight:300;margin-bottom:2rem;margin-top:0}h1{font-size:4rem;letter-spacing:-0.1rem;line-height:1.2}h2{font-size:3.6rem;letter-spacing:-0.1rem;line-height:1.25}h3{font-size:3rem;letter-spacing:-0.1rem;line-height:1.3}h4{font-size:2.4rem;letter-spacing:-0.08rem;line-height:1.35}h5{font-size:1.8rem;letter-spacing:-0.05rem;line-height:1.5}h6{font-size:1.6rem;letter-spacing:0;line-height:1.4}@media (min-width: 40rem){h1{font-size:5rem}h2{font-size:4.2rem}h3{font-size:3.6rem}h4{font-size:3rem}h5{font-size:2.4rem}h6{font-size:1.5rem}}.float-right{float:right}.float-left{float:left}.clearfix{*zoom:1}.clearfix:after,.clearfix:before{content:"";display:table}.clearfix:after{clear:both}

.date-menu li {
	display: inline-block;
}

.error-box {
	color: red;
	background-color: #fee;
	border: 1px solid red;
	padding: 1em;
	margin-bottom: 1em;
}

textarea.code {
	font-family: monospace;
	height: 10em;
}

.thumbnail {
	width: <?php echo $config['thumbnailWidth']; ?>px;
	height: <?php echo $config['thumbnailHeight']; ?>px;
}
</style>
</head>

<?php if ($errorMessage): ?>
	<div class="error-box"><?php echo htmlentities(_t('Error: %s', $errorMessage)); ?></div>
<?php endif; ?>

<?php // --------------------------------------------------------------- ?>
<?php // CONFIGURATION HELPER SCREEN                                     ?>
<?php // --------------------------------------------------------------- ?>

<?php if (!$config): ?>
	<h3><?php echo htmlentities(pmcctv_appName()); ?></h3>

	<?php if ($configToDisplay): ?>
		<p><?php echo _t('Create a new "config.php" file with the following content, and place it next to index.php:'); ?></p>
		<p><strong>config.php</strong></p>
		<textarea class="code">&lt;?php if (!defined('IS_PMCCTV_SERVER') || !IS_PMCCTV_SERVER) die('Unauthorized'); return <?php echo var_export($configToDisplay, JSON_PRETTY_PRINT); ?>;</textarea>
	<?php else: ?>
		<p><?php echo _t('No configuration file has been detected. Input your information below to create it:'); ?></p>
		<form method="post" action="">
			<label for="username"><?php echo _t('Username'); ?></label><input name="username" type="text" placeholder="Your name" value="<?php echo htmlentities(isset($_POST['username']) ? $_POST['username'] : ''); ?>" />
			<label for="password"><?php echo _t('Password'); ?></label><input name="password" type="password" placeholder="**********" value="<?php echo htmlentities(isset($_POST['password']) ? $_POST['password'] : ''); ?>" />
			<label for="captureDir"><?php echo _t('Path to directory where captured images and videos are stored'); ?></label><input name="captureDir" type="text" placeholder="/path/to/capture/dir" value="<?php echo htmlentities(isset($_POST['captureDir']) ? $_POST['captureDir'] : ''); ?>" />
			<label for="captureBaseUrl"><?php echo _t('URL to directory where images and videos are stored'); ?></label><input name="captureBaseUrl" type="text" placeholder="https://example.com/capture/dir" value="<?php echo htmlentities(isset($_POST['captureBaseUrl']) ? $_POST['captureBaseUrl'] : ''); ?>" />
			<input name="create_config" type="submit" value="<?php echo _t('Create configuration'); ?>"/>
		</form>
	<?php endif; ?>

<?php // --------------------------------------------------------------- ?>
<?php // LOGIN SCREEN                                                    ?>
<?php // --------------------------------------------------------------- ?>

<?php elseif (!$_SESSION['logged_in']): ?>
	<h3><?php echo htmlentities(pmcctv_appName()); ?></h3>
	<form method="post" action="">
		<label for="username"><?php echo _t('Username'); ?></label><input name="username" type="text" />
		<label for="password"><?php echo _t('Password'); ?></label><input name="password" type="password" />
		<input name="login" type="submit" value="<?php echo _t('Login'); ?>"/>
	</form>

<?php // --------------------------------------------------------------- ?>
<?php // MAIN SCREEN                                                     ?>
<?php // --------------------------------------------------------------- ?>

<?php else: ?>
	<div>
		<div style="float: left;">
			<h3><?php echo htmlentities(pmcctv_appName()); ?></h3>
		</div>
		<div style="float: right;">
			<form method="post" action="">
				<input name="logout" type="submit" value="<?php echo _t('Logout'); ?>"/>
			</form>
		</div>
	</div>

	<div style="clear: both;"></div>

	<?php $previousDate = null; ?>
	<ul class="date-menu">
		<?php foreach ($capturedFiles as $file): ?>
			<?php $date = $file['time']->format('Ymd'); ?>
			<?php if ($previousDate == $date) continue; ?>
			<?php $previousDate = $date; ?>
			<li>
				<a class="button <?php echo $date == $selectedDate->format('Ymd') ? '' : 'button-outline'; ?>" href="?date=<?php echo rawurlencode($date); ?>">
					<?php echo htmlentities(pmcctv_friendlyDate($file['time'])); ?>
				</a>
			</li>
		<?php endforeach; ?>
	</ul>

	<ul class="captured-images">
		<?php if ($selectedDate): ?>
			<?php foreach ($capturedFiles as $file): ?>
				<?php if ($selectedDate->format('Ymd') != $file['time']->format('Ymd')) continue; ?>
				<li>
					<strong><?php echo htmlentities(pmcctv_friendlyDateTime($file['time'])); ?></strong><br/>
					<?php if (pmcctv_mediaType($file['path']) == 'image'): ?>
						<img class="thumbnail" src="<?php echo htmlentities($config['captureBaseUrl'] . '/' . basename($file['path'])); ?>" />
					<?php else: ?>
						<video class="thumbnail" controls>
							<source src="<?php echo htmlentities($config['captureBaseUrl'] . '/' . basename($file['path'])); ?>" type="video/ogg">
							Your browser does not support the video tag.
						</video>
					<?php endif; ?>
				</li>
			<?php endforeach; ?>
		<?php endif; ?>
	</ul>
<?php endif; ?>

</html>
